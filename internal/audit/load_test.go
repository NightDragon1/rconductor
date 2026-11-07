// SPDX-License-Identifier: AGPL-3.0-or-later

package audit_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestV3AuditNotLostUnderLock covers V-3: hundreds of writers at once, while
// another writer holds the database longer than SQLite waits on its own.
// Entries written outside a transaction have no caller left to report a
// failure to, so each of them must arrive.
func TestV3AuditNotLostUnderLock(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET key = key`); err != nil {
		t.Fatal(err)
	}
	const writers = 500
	var failed atomic.Int32
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			// A client that hangs up does not end the entry either.
			c, cancel := context.WithCancel(ctx)
			if i%2 == 0 {
				cancel()
			}
			defer cancel()
			if _, err := audit.Write(c, st.DB, audit.Entry{Kind: audit.KindAuth, Action: fmt.Sprintf("load.%d", i), Result: audit.ResultDenied}); err != nil {
				failed.Add(1)
				t.Log(err)
			}
		})
	}
	time.Sleep(6 * time.Second) // past the busy timeout of 5 s
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	var n int
	if err := st.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action LIKE 'load.%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != writers || failed.Load() != 0 {
		t.Errorf("%d of %d entries written, %d failed", n, writers, failed.Load())
	}
}

// TestV3WriteInsideATransactionFailsAtOnce: a transaction has a caller that
// rolls back, the retry is only for entries on their own.
func TestV3WriteInsideATransactionFailsAtOnce(t *testing.T) {
	st := storetest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	tx, err := st.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	cancel()
	if _, err := audit.Write(ctx, tx, audit.Entry{Kind: audit.KindAuth, Action: "x", Result: audit.ResultOK}); err == nil {
		t.Error("canceled write in a transaction went through")
	}
}

// TestB7StopGivesLockedEntriesToTheLog: at shutdown an entry that waits for
// a locked database goes to the log at once instead of being lost with the
// process.
func TestB7StopGivesLockedEntriesToTheLog(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	var logs bytes.Buffer
	audit.SetLostLog(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { audit.SetLostLog(nil) })
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET key = key`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := audit.Write(ctx, st.DB, audit.Entry{Kind: audit.KindAuth, Action: "stop.test", Result: audit.ResultDenied})
		done <- err
	}()
	audit.Stop()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("written through a lock")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("still waiting after Stop")
	}
	if !strings.Contains(logs.String(), "stop.test") {
		t.Errorf("not in the log: %s", logs.String())
	}
}
