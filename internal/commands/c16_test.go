// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/users"
)

// heldExec answers the first command only once released.
type heldExec struct {
	asked, release chan struct{}
	once           sync.Once
}

func (h *heldExec) Execute(context.Context, string, string) (string, error) {
	first := false
	h.once.Do(func() { first = true })
	if first {
		close(h.asked)
		<-h.release
	}
	return "done", nil
}

// TestC16TimeBudget covers CMD-010 and D-069 with the time budget: a
// server whose turn comes after the budget is not sent the command; it
// gets result skipped with reason time_budget and its own request ID, and
// exactly one audit entry each, an error with that reason under the batch.
func TestC16TimeBudget(t *testing.T) {
	w := newWorld(t)
	b, c := w.second(t, "b"), w.second(t, "c")
	w.setting(users.SettingMultiParallel, 1)
	var mu sync.Mutex
	clock := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	h := &heldExec{asked: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(w.st, w.c, h, w.svc.log)
	svc.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	type out struct {
		batch string
		items []MultiItem
		err   error
	}
	done := make(chan out, 1)
	go func() {
		batch, items, err := svc.Multi(context.Background(), w.as("admin"), []string{w.server, b, c}, "list")
		done <- out{batch, items, err}
	}()
	<-h.asked
	// The first run holds the only slot past the deadline.
	mu.Lock()
	clock = clock.Add(multiAnswerBy)
	mu.Unlock()
	close(h.release)
	r := <-done
	if r.err != nil || len(r.items) != 3 || r.items[0].Result != ResultOK {
		t.Fatalf("multi = %+v, %v", r.items, r.err)
	}
	for _, it := range r.items[1:] {
		if it.Result != ResultSkipped || it.Reason != ReasonTimeBudget || it.RequestID == "" {
			t.Errorf("skipped server = %+v", it)
		}
		var n int
		var result, payload string
		if err := w.st.DB.QueryRow(`SELECT count(*), max(result), max(payload) FROM audit_log WHERE id = ? AND reason_code = 'time_budget'`, it.RequestID).Scan(&n, &result, &payload); err != nil {
			t.Fatal(err)
		}
		if n != 1 || result != "error" || !strings.Contains(payload, r.batch) {
			t.Errorf("audit of %s: %d entries, result %q, payload %s", it.ServerID, n, result, payload)
		}
		var sent int
		_ = w.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE server_id = ? AND action = 'cmd.sent'`, it.ServerID).Scan(&sent)
		if sent != 0 {
			t.Errorf("%s was sent the command", it.ServerID)
		}
	}
	var all int
	_ = w.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code = 'time_budget'`).Scan(&all)
	if all != 2 {
		t.Errorf("%d time budget entries, want one per skipped server", all)
	}
}

// TestC16OutsideScopeAfterBudget: a server outside the scope stays
// not_found after the budget, without an audit entry (D-069).
func TestC16OutsideScopeAfterBudget(t *testing.T) {
	w := newWorld(t)
	svc := NewService(w.st, w.c, &heldExec{asked: make(chan struct{}), release: make(chan struct{})}, w.svc.log)
	svc.SetTimeout(multiAnswerBy) // no time to start any run
	_, items, err := svc.Multi(context.Background(), w.as("sa"), []string{w.server, "00000000-0000-4000-8000-00000000dead"}, "list")
	if err != nil || len(items) != 2 || items[1].Result != ResultNotFound || items[1].RequestID != "" {
		t.Fatalf("multi = %+v, %v", items, err)
	}
	var n int
	_ = w.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code = 'time_budget' AND server_id = ?`, items[1].ServerID).Scan(&n)
	if n != 0 {
		t.Errorf("%d audit entries for a server outside the scope", n)
	}
}
