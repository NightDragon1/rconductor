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
// not_found after the budget, without an audit entry (D-069); the first
// server still runs.
func TestC16OutsideScopeAfterBudget(t *testing.T) {
	w := newWorld(t)
	h := &heldExec{asked: make(chan struct{}), release: make(chan struct{})}
	close(h.release) // a run that starts after all ends at once
	svc := NewService(w.st, w.c, h, w.svc.log)
	svc.SetTimeout(multiAnswerBy) // no time to start any run
	_, items, err := svc.Multi(context.Background(), w.as("sa"), []string{w.server, "00000000-0000-4000-8000-00000000dead"}, "list")
	if err != nil || len(items) != 2 || items[0].Result != ResultOK || items[1].Result != ResultNotFound || items[1].RequestID != "" {
		t.Fatalf("multi = %+v, %v", items, err)
	}
	var n int
	_ = w.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE reason_code = 'time_budget' AND server_id = ?`, items[1].ServerID).Scan(&n)
	if n != 0 {
		t.Errorf("%d audit entries for a server outside the scope", n)
	}
	if !strings.Contains(w.logs.String(), `"reason":"not_found"`) {
		t.Error("no denial logged for the server outside the scope")
	}
}

// TestC16SkippedAuditOutlivesTheClient: the audit entry of a left-out
// server is written also when the client has gone, with the input cut as
// for a refused one, and the console is told.
func TestC16SkippedAuditOutlivesTheClient(t *testing.T) {
	w := newWorld(t)
	b := w.second(t, "b")
	h := &heldExec{asked: make(chan struct{}), release: make(chan struct{})}
	close(h.release)
	svc := NewService(w.st, w.c, h, w.svc.log)
	svc.SetTimeout(multiAnswerBy)
	var told []Entry
	var mu sync.Mutex
	svc.Publish = func(_ string, e Entry) {
		if e.Reason == ReasonTimeBudget {
			mu.Lock()
			told = append(told, e)
			mu.Unlock()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, all, err := svc.Multi(ctx, w.as("admin"), []string{w.server, b}, strings.Repeat("say x ", 1000))
	if err != nil || len(all) != 2 || all[1].Result != ResultSkipped {
		t.Fatalf("multi = %+v, %v", all, err)
	}
	items := all[1:]
	var payload string
	if err := w.st.DB.QueryRow(`SELECT payload FROM audit_log WHERE id = ? AND reason_code = 'time_budget'`, items[0].RequestID).Scan(&payload); err != nil {
		t.Fatalf("no audit entry after the client left: %v", err)
	}
	if len(payload) > 600 {
		t.Errorf("payload of %d bytes", len(payload))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(told) != 1 || told[0].Reason != ReasonTimeBudget || told[0].RequestID != items[0].RequestID {
		t.Errorf("console told %+v", told)
	}
}

// TestC16FirstServerAlwaysRuns: with a command timeout that fills the
// whole budget, the first server still gets the command; only the further
// servers are skipped with reason time_budget (CMD-010, D-069).
func TestC16FirstServerAlwaysRuns(t *testing.T) {
	w := newWorld(t)
	b, c := w.second(t, "b"), w.second(t, "c")
	h := &heldExec{asked: make(chan struct{}), release: make(chan struct{})}
	close(h.release)
	svc := NewService(w.st, w.c, h, w.svc.log)
	svc.SetTimeout(multiAnswerBy + time.Second)
	_, items, err := svc.Multi(context.Background(), w.as("admin"), []string{w.server, b, c}, "list")
	if err != nil || len(items) != 3 || items[0].Result != ResultOK {
		t.Fatalf("multi = %+v, %v", items, err)
	}
	for _, it := range items[1:] {
		if it.Result != ResultSkipped || it.Reason != ReasonTimeBudget {
			t.Errorf("further server = %+v", it)
		}
	}
	select {
	case <-h.asked:
	default:
		t.Error("the first server was not sent the command")
	}
}
