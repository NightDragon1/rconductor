// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package syslog keeps the system log of the panel (D-091): events from
// info up, blacked out (SEC-012), masked (D-088), stored in batches; it
// never blocks the panel, a line not kept is counted.
package syslog

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Retention is how long an entry stays (D-091).
const Retention = 7 * 24 * time.Hour

// Queue and batch sizes, write and trim intervals.
const (
	queueSize  = 4096
	batchSize  = 256
	flushEvery = time.Second
	pruneEvery = time.Minute
	pruneStep  = 500
	maxCode    = 64
)

// Masker masks lines as for container logs (logmask.Provider).
type Masker interface {
	Lines(lines []string, profilePatterns [][]string) ([]string, int)
}

// serverMasker also knows the profile patterns of a server (D-088 b): an
// entry about a server is masked with them too; one without a server only
// with the built-in list and the texts of the RCON admin.
type serverMasker interface {
	PatternsFor(ctx context.Context, serverID string) [][]string
}

// Sink receives the JSON lines of the log handler.
type Sink struct {
	queue chan []byte
	lost  atomic.Int64
	// failing: the last write failed (logged once; writer goroutine only).
	failing bool
	// told is the number of lost lines reported so far (writer goroutine).
	told int64
}

// NewSink returns an empty sink; lines wait in the queue until Run.
func NewSink() *Sink { return &Sink{queue: make(chan []byte, queueSize)} }

// Write takes one line; with a full queue it is counted as lost.
func (s *Sink) Write(p []byte) (int, error) {
	select {
	case s.queue <- bytes.Clone(p):
	default:
		s.lost.Add(1)
	}
	return len(p), nil
}

// Lost counts the lines that were not kept.
func (s *Sink) Lost() int64 { return s.lost.Load() }

// Run writes and trims until ctx ends, then writes what is left.
func (s *Sink) Run(ctx context.Context, st *store.Store, m Masker, log *slog.Logger, now func() time.Time) {
	flush, prune := time.NewTicker(flushEvery), time.NewTicker(pruneEvery)
	defer flush.Stop()
	defer prune.Stop()
	s.prune(ctx, st, log, now())
	for {
		select {
		case <-ctx.Done():
			s.drain(context.WithoutCancel(ctx), st, m, log)
			return
		case <-flush.C:
			s.drain(ctx, st, m, log)
		case <-prune.C:
			s.prune(ctx, st, log, now())
		}
	}
}

// drain writes what is queued now, one batch after the other.
func (s *Sink) drain(ctx context.Context, st *store.Store, m Masker, log *slog.Logger) {
	for {
		var lines []string
		for len(lines) < batchSize {
			select {
			case b := <-s.queue:
				lines = append(lines, string(bytes.TrimRight(b, "\n")))
				continue
			default:
			}
			break
		}
		if len(lines) == 0 {
			s.tellLost(log)
			return
		}
		if err := s.write(ctx, st, m, lines); err != nil {
			s.lost.Add(int64(len(lines)))
			if !s.failing {
				log.Warn("system log not written", slog.Any("err", err))
			}
			s.failing = true
		} else {
			s.failing = false
		}
	}
}

// tellLost reports the lines lost since the last report, at most once per
// drain, with the event code (D-077, D-091). While writes fail it waits:
// its own line would be lost too and keep the report going; the next
// drain that writes tells the number.
func (s *Sink) tellLost(log *slog.Logger) {
	all := s.Lost()
	if all == s.told || s.failing {
		return
	}
	log.Warn("system log lines lost", logging.Event(logging.SyslogLinesLost), slog.Int64("lost", all-s.told), slog.Int64("lost_total", all))
	s.told = all
}

func (s *Sink) write(ctx context.Context, st *store.Store, m Masker, lines []string) error {
	// Masked before the write lock: the patterns of a server may need a read.
	rows := make([]sqlitedb.InsertSystemLogParams, 0, len(lines))
	for _, line := range lines {
		h := headOf(line)
		var patterns [][]string
		if sm, ok := m.(serverMasker); ok && h.Server != "" {
			if id, err := uuid.Parse(h.Server); err == nil {
				patterns = sm.PatternsFor(ctx, id.String())
			}
		}
		text, _ := maskLine(line, m, patterns)
		if len(h.Event) > maxCode {
			h.Event = ""
		}
		rows = append(rows, sqlitedb.InsertSystemLogParams{Ts: h.TS, Level: h.Level, Code: h.Event, Text: text, Size: int64(len(text))})
	}
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	for _, r := range rows {
		if err := q.InsertSystemLog(ctx, r); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type head struct{ TS, Level, Event, Server string }

// headOf reads ts, level and event of a JSON line from their first
// occurrence: the handler writes them first, so an attribute of the same
// name later in the line cannot replace them.
func headOf(line string) head {
	var h head
	dec := json.NewDecoder(strings.NewReader(line))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return h
	}
	seen := map[string]bool{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return h
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return h
		}
		key, _ := k.(string)
		if seen[key] {
			continue
		}
		seen[key] = true
		var str string
		if json.Unmarshal(v, &str) != nil {
			continue
		}
		switch key {
		case "ts":
			h.TS = str
		case "level":
			h.Level = str
		case "event":
			h.Event = str
		case logging.KeyServer:
			h.Server = str
		}
	}
	return h
}

// MaskLine masks a log line through Rewrite, such as the agent's own log
// (D-091).
func MaskLine(line string, m Masker) string {
	out, _ := MaskLineCounted(line, m)
	return out
}

// MaskLineCounted is MaskLine with the number of values the masker held
// back for its time budget.
func MaskLineCounted(line string, m Masker) (string, int) {
	return maskLine(line, m, nil)
}

// maskLine masks a line value by value with the given profile patterns.
func maskLine(line string, m Masker, patterns [][]string) (string, int) {
	held := 0
	out := Rewrite(line, func(texts []string) []string {
		out, n := m.Lines(texts, patterns)
		held += n
		return out
	})
	return out, held
}

// Rewrite hands the text values of an encoded line or document to f, in a
// fixed order and as they were logged, and puts f's answers in their place:
// JSON is decoded (numbers kept exact) and encoded again without HTML
// escaping; any other text goes to f whole, without its escaped quotes
// (slog text). Every sink of logs and of the diagnostic export masks,
// blacks out and pseudonymizes through it (D-088, D-090, D-091).
func Rewrite(line string, f func([]string) []string) string {
	var v any
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	if !json.Valid([]byte(line)) || dec.Decode(&v) != nil {
		return f([]string{strings.ReplaceAll(line, `\"`, `"`)})[0]
	}
	var texts []string
	walk(v, func(t string) string { texts = append(texts, t); return t })
	out := f(texts)
	v = walk(v, func(string) string { t := out[0]; out = out[1:]; return t })
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}

// walk replaces every text in v, object keys included (foreign JSON may
// carry a host as a key), in a fixed order: per key the key, then its
// value. A key that becomes another's gets ~2, ~3 and so on.
func walk(v any, f func(string) string) any {
	switch x := v.(type) {
	case string:
		return f(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		next := map[string]int{} // the next counter per key that met another
		for _, k := range slices.Sorted(maps.Keys(x)) {
			nk := f(k)
			val := walk(x[k], f)
			if _, taken := out[nk]; taken {
				base := nk
				for n := max(next[base], 2); ; n++ {
					nk = base + "~" + strconv.Itoa(n)
					if _, used := out[nk]; !used {
						next[base] = n + 1
						break
					}
				}
			}
			out[nk] = val
		}
		return out
	case []any:
		for i := range x {
			x[i] = walk(x[i], f)
		}
	}
	return v
}

// prune deletes entries past Retention, then the oldest above the size.
func (s *Sink) prune(ctx context.Context, st *store.Store, log *slog.Logger, now time.Time) {
	q := st.Queries
	if _, err := q.PurgeSystemLogBefore(ctx, now.Add(-Retention).UTC().Format(store.TimeLayout)); err != nil {
		log.Warn("system log not trimmed", slog.Any("err", err))
		return
	}
	limit := users.DefaultSystemLogMaxBytes
	if err := st.GuardedSetting(ctx, log, users.SettingSystemLogMaxBytes, &limit); err != nil || limit < users.MinSystemLogMaxBytes {
		limit = users.DefaultSystemLogMaxBytes
	}
	for {
		size, err := q.SystemLogBytes(ctx)
		if err != nil || size <= int64(limit) {
			return
		}
		if n, err := q.DeleteOldestSystemLog(ctx, pruneStep); err != nil || n == 0 {
			return
		}
	}
}
