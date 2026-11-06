// SPDX-License-Identifier: AGPL-3.0-or-later

package audit

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
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

// TestEntryOutlastsALockedDatabase covers V-3 at its limit: if the database
// stays locked past writeWait the entry is not gone, it stands in the log in
// full.
func TestEntryOutlastsALockedDatabase(t *testing.T) {
	defer func(d time.Duration) { writeWait = d }(writeWait)
	writeWait = 200 * time.Millisecond
	defer SetLostLog(nil)
	var logs bytes.Buffer
	SetLostLog(slog.New(slog.NewJSONHandler(&logs, nil)))
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
	_, err = Write(ctx, st.DB, Entry{Kind: KindAuth, Action: "login", ActorID: "u1", Payload: map[string]string{"ip": "192.0.2.1"}, Result: ResultDenied, Reason: "rate_limited"})
	if err == nil {
		t.Fatal("write went through a locked database")
	}
	for _, want := range []string{`"event":"audit.entry_lost"`, `"action":"login"`, `"actor_id":"u1"`, `192.0.2.1`, `"reason":"rate_limited"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("%s missing in %s", want, logs.String())
		}
	}
}
