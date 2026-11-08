// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"net/http"
	"sync"
	"testing"
)

// TestMultiThroughTheStack covers CMD-010 over HTTP: results per server,
// a server outside the scope looks like an unknown one, a batch_id or any
// other unknown field in the request is refused, missing fields too;
// parallel calls each get their own batch.
func TestMultiThroughTheStack(t *testing.T) {
	f := newRightsFixture(t)
	type item struct {
		ServerID  string `json:"server_id"`
		RequestID string `json:"request_id"`
		Result    string `json:"result"`
		Reason    string `json:"reason"`
	}
	var out struct {
		BatchID string `json:"batch_id"`
		Items   []item
	}
	f.as(rSI).must(http.MethodPost, "/commands/multi", map[string]any{
		"server_ids": []string{f.server, f.server2, "01900000-0000-7000-8000-000000000000"}, "command": "say hi",
	}, &out, http.StatusOK)
	if out.BatchID == "" || len(out.Items) != 3 || out.Items[0].Result != "ok" || out.Items[0].RequestID == "" {
		t.Fatalf("multi = %+v", out)
	}
	// server2 is outside the scope of sa_in: the same answer as an unknown ID.
	if out.Items[1] != (item{ServerID: f.server2, Result: "not_found", Reason: "not_found"}) ||
		out.Items[2].Result != "not_found" || out.Items[2].RequestID != "" {
		t.Errorf("outside the scope = %+v, unknown = %+v", out.Items[1], out.Items[2])
	}

	for _, body := range []any{
		map[string]any{"server_ids": []string{f.server}, "command": "say hi", "batch_id": "01900000-0000-7000-8000-000000000001"},
		map[string]any{"server_ids": []string{f.server}, "command": "say hi", "extra": 1},
		map[string]any{"server_ids": []string{f.server}},
		map[string]any{"command": "say hi"},
		map[string]any{"server_ids": []string{}, "command": "say hi"},
		map[string]any{"server_ids": []string{"not-a-uuid"}, "command": "say hi"},
	} {
		if st, reason := f.raw(rA, http.MethodPost, "/commands/multi", body); st != http.StatusBadRequest && st != http.StatusUnprocessableEntity {
			t.Errorf("%v: %d %s", body, st, reason)
		}
	}
	if st, reason := f.raw(rMR, http.MethodPost, "/commands/multi", map[string]any{"server_ids": []string{f.server}, "command": "say hi"}); st != http.StatusForbidden {
		t.Errorf("moderator: %d %s", st, reason)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	batches := map[string]bool{}
	for range 6 {
		wg.Go(func() {
			var o struct {
				BatchID string `json:"batch_id"`
			}
			if f.as(rA).call(http.MethodPost, "/commands/multi", map[string]any{"server_ids": []string{f.server}, "command": "list"}, &o) == http.StatusOK {
				mu.Lock()
				batches[o.BatchID] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(batches) != 6 {
		t.Errorf("parallel calls gave %d batches, want 6", len(batches))
	}
}
