// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const fullMsg = "recovery limit table full"

type fullEntry struct {
	TS    time.Time `json:"ts"`
	Count int       `json:"count"`
}

// fullEntries returns the reports of a full client table in the logs.
func fullEntries(t *testing.T, logs string) []fullEntry {
	t.Helper()
	var es []fullEntry
	for line := range strings.SplitSeq(logs, "\n") {
		if !strings.Contains(line, fullMsg) {
			continue
		}
		var e fullEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		es = append(es, e)
	}
	return es
}

// TestRecoveryFullReportPerWindowC39: while the client table stays full,
// each new client that shares the overflow window counts; the report goes
// out at most once per window with the number since the last one, and the
// reports together count every such client, also those of the last window
// (AUTH-008). Windows end one by one, so each round one new client takes
// the freed place and the next ones share the overflow window.
func TestRecoveryFullReportPerWindowC39(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSFFixture(t)
		ctx := context.Background()
		ip := netip.MustParseAddr("10.0.0.1")
		take := func() {
			f.now = time.Now()
			f.svc.takeRecoveryClient(ctx, ip)
			ip = ip.Next()
		}
		// Fill the table with windows that start 300 ms apart.
		const step = 300 * time.Millisecond
		start := time.Now()
		for i := range maxRecoveryKeys {
			f.svc.sf.clients[ip] = &recoveryWindow{start: start.Add(time.Duration(i) * step), count: 1}
			ip = ip.Next()
		}
		// From the end of the first window on, one window ends per round.
		time.Sleep(time.Hour + step/2)
		const rounds = 1000 // 300 s, five report windows
		shared := 0
		for i := range rounds {
			take() // takes the freed place
			for range 1 + i%3 {
				take()
				shared++
			}
			time.Sleep(step)
		}
		time.Sleep(2 * fullReportGap)
		synctest.Wait()
		f.svc.WaitMail()
		es := fullEntries(t, f.logs.String())
		total := 0
		for i, e := range es {
			if i > 0 && e.TS.Sub(es[i-1].TS) < fullReportGap {
				t.Errorf("entries %v apart, window %v", e.TS.Sub(es[i-1].TS), fullReportGap)
			}
			total += e.Count
		}
		if total != shared {
			t.Errorf("new clients on the full table counted %d in %d entries, want %d", total, len(es), shared)
		}
		// 300 s of new clients make at most one entry per begun window.
		if len(es) < 2 || len(es) > 6 {
			t.Errorf("entries = %d, want one per window", len(es))
		}
	})
}
