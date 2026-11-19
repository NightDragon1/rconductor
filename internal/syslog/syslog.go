// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package syslog keeps the system log of the panel (D-091, UI-025): every
// event from info up, as the log handler wrote it (blacked out after
// SEC-012), masked as in D-088 and stored in batches. Writing never blocks
// the panel: a line that cannot be kept is counted and dropped.
package syslog

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Retention is how long an entry stays (D-091).
const Retention = 7 * 24 * time.Hour

// Sizes of the queue and of one batch, and how often the queue is written
// and the log trimmed.
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
	// failing is set after a failed write until the next one succeeds, so
	// the failure is logged once (only the writer goroutine uses it).
	failing bool
}

// NewSink returns an empty sink; lines wait in its queue until Run.
func NewSink() *Sink { return &Sink{queue: make(chan []byte, queueSize)} }

// Write takes one line. It never blocks: with a full queue the line is
// counted as lost.
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

// Run writes the queue in batches and trims the log until ctx ends.
func (s *Sink) Run(ctx context.Context, st *store.Store, m Masker, log *slog.Logger, now func() time.Time) {
	flush, prune := time.NewTicker(flushEvery), time.NewTicker(pruneEvery)
	defer flush.Stop()
	defer prune.Stop()
	s.prune(ctx, st, log, now())
	for {
		select {
		case <-ctx.Done():
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
	masked, _ := m.Lines(lines, nil)
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	for i, line := range lines {
		var head struct {
			TS    string `json:"ts"`
			Level string `json:"level"`
			Event string `json:"event"`
		}
		_ = json.Unmarshal([]byte(line), &head)
		if len(head.Event) > maxCode {
			head.Event = ""
		}
		text := masked[i]
		if err := q.InsertSystemLog(ctx, sqlitedb.InsertSystemLogParams{
			Ts: head.TS, Level: head.Level, Code: head.Event, Text: text, Size: int64(len(text)),
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// prune deletes entries older than Retention, then the oldest ones while
// the log is above system_log.max_bytes.
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
