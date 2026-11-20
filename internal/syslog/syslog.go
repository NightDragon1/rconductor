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
	"strings"
	"sync/atomic"
	"time"

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

// Sink receives the JSON lines of the log handler.
type Sink struct {
	queue chan []byte
	lost  atomic.Int64
	// failing: the last write failed (logged once; writer goroutine only).
	failing bool
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
			return
		}
		if err := s.write(ctx, st, m, lines); err != nil {
			s.lost.Add(int64(len(lines)))
			if !s.failing {
				log.Warn("system log not written", slog.Int64("lost", s.Lost()), slog.Any("err", err))
			}
			s.failing = true
		} else {
			s.failing = false
		}
	}
}

func (s *Sink) write(ctx context.Context, st *store.Store, m Masker, lines []string) error {
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	for _, line := range lines {
		text, head := maskJSON(line, m)
		if len(head.Event) > maxCode {
			head.Event = ""
		}
		if err := q.InsertSystemLog(ctx, sqlitedb.InsertSystemLogParams{
			Ts: head.TS, Level: head.Level, Code: head.Event, Text: text, Size: int64(len(text)),
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type head struct{ TS, Level, Event string }

// maskJSON masks each text value of a JSON line on its own, as it was
// logged and before the encoding escapes its quotes (D-088), and encodes
// the line again; a line that is not JSON is masked as it is.
func maskJSON(line string, m Masker) (string, head) {
	var rec map[string]any
	if json.Unmarshal([]byte(line), &rec) != nil {
		out, _ := m.Lines([]string{line}, nil)
		return out[0], head{}
	}
	var texts []string
	walk(rec, func(t string) string { texts = append(texts, t); return t })
	masked, _ := m.Lines(texts, nil)
	walk(rec, func(string) string { t := masked[0]; masked = masked[1:]; return t })
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(rec)
	h := head{}
	h.TS, _ = rec["ts"].(string)
	h.Level, _ = rec["level"].(string)
	h.Event, _ = rec["event"].(string)
	return strings.TrimRight(b.String(), "\n"), h
}

// MaskLine masks a JSON log line value by value as maskJSON does, for log
// lines that arrive encoded, such as the agent's own log (D-091).
func MaskLine(line string, m Masker) string {
	out, _ := maskJSON(line, m)
	return out
}

// walk replaces every text value in v, in a fixed order.
func walk(v any, f func(string) string) any {
	switch x := v.(type) {
	case string:
		return f(x)
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			x[k] = walk(x[k], f)
		}
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
