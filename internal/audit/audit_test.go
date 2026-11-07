// SPDX-License-Identifier: AGPL-3.0-or-later

package audit

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

func migrated(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	log := logging.New(io.Discard, "panel", config.Log{}).Module("db")
	if _, err := s.Migrate(ctx, log, store.MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWrite(t *testing.T) {
	s := migrated(t)
	ctx := context.Background()

	id, err := Write(ctx, s.DB, Entry{
		Kind: KindAuth, ActorID: "u1", ActorRole: "rcon_admin", SessionID: "abcd",
		Action: "login", Payload: map[string]string{"ip": "192.0.2.1"}, Result: ResultOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	var kind, action, payload, result string
	var server, reason sql.NullString
	if err := s.DB.QueryRowContext(ctx,
		`SELECT kind, action, payload, result, server_id, reason_code FROM audit_log WHERE id = ?`, id,
	).Scan(&kind, &action, &payload, &result, &server, &reason); err != nil {
		t.Fatal(err)
	}
	if kind != "auth" || action != "login" || result != "ok" || payload != `{"ip":"192.0.2.1"}` {
		t.Errorf("entry = %s %s %s %s", kind, action, result, payload)
	}
	if server.Valid || reason.Valid {
		t.Errorf("empty fields must be NULL: server=%v reason=%v", server, reason)
	}

	// A given ID (request ID of a command) is kept.
	if got, err := Write(ctx, s.DB, Entry{ID: "req-1", Kind: KindAdmin, Action: "x", Result: ResultDenied, Reason: "not_found"}); err != nil || got != "req-1" {
		t.Errorf("Write with ID = %q, %v", got, err)
	}
}

func TestWriteInTransaction(t *testing.T) {
	s := migrated(t)
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(ctx, tx, Entry{Kind: KindAdmin, Action: "user.update", Result: ResultOK}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rolled back entry persisted: n=%d err=%v", n, err)
	}
}

func TestInvalidResultRejected(t *testing.T) {
	s := migrated(t)
	if _, err := Write(context.Background(), s.DB, Entry{Kind: KindAuth, Action: "x", Result: "maybe"}); err == nil {
		t.Error("invalid result accepted")
	}
}

// TestR3B6EntryOutlastsALockedDatabase: if the database stays locked past
// writeWait the entry is not gone, it stands in the log in full.
func TestR3B6EntryOutlastsALockedDatabase(t *testing.T) {
	defer func(d time.Duration) { writeWait = d }(writeWait)
	writeWait = 200 * time.Millisecond
	defer SetLostLog(nil)
	var logs bytes.Buffer
	SetLostLog(logging.New(&logs, "panel", config.Log{Format: "json"}).Root())
	st := storetest.New(t)
	ctx := context.Background()
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET key = key`); err != nil {
		t.Fatal(err)
	}
	_, err = Write(ctx, st.DB, Entry{Kind: KindAuth, Action: "login", ActorID: "u1", SessionID: "hash1", Payload: map[string]string{"ip": "192.0.2.1"}, Result: ResultDenied, Reason: "rate_limited"})
	if err == nil {
		t.Fatal("write went through a locked database")
	}
	for _, want := range []string{`"event":"audit.entry_lost"`, `"action":"login"`, `"actor_id":"u1"`, `"session_hash":"hash1"`, `192.0.2.1`, `"reason":"rate_limited"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("%s missing in %s", want, logs.String())
		}
	}
}

// lockedBuffer is a log for parallel writers.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestR4B6LostEntriesKeepTheirSession: with the real log, which hides the key
// session_id, each entry stands there with the hash of its session. URL
// credentials in a payload stay hidden, on purpose (SEC-012).
func TestR4B6LostEntriesKeepTheirSession(t *testing.T) {
	defer func(d time.Duration) { writeWait = d }(writeWait)
	writeWait = 700 * time.Millisecond
	defer SetLostLog(nil)
	logs := &lockedBuffer{}
	SetLostLog(logging.New(logs, "panel", config.Log{Format: "json"}).Root())
	st := storetest.New(t)
	ctx := context.Background()
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET key = key`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			c, cancel := context.WithCancel(ctx)
			if i%2 == 0 {
				cancel()
			}
			defer cancel()
			_, _ = Write(c, st.DB, Entry{Kind: KindCommand, Action: fmt.Sprintf("lost.%d", i), SessionID: "deadbeefhash",
				Payload: map[string]string{"command": "say https://bob:pw@host/x"}, Result: ResultOK})
		})
	}
	wg.Wait()
	got := logs.String()
	if n := strings.Count(got, "audit.entry_lost"); n != 100 {
		t.Errorf("%d of 100 entries in the log", n)
	}
	if n := strings.Count(got, `"session_hash":"deadbeefhash"`); n != 100 {
		t.Errorf("%d of 100 entries keep their session", n)
	}
	if strings.Contains(got, "bob:pw") {
		t.Error("credentials in the log (SEC-012)")
	}
}
