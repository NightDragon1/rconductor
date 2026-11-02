// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"testing"
	"time"
)

// TestBackoffJitter: 1 s growing by 1.6, capped at MaxBackoff, each wait
// varied by ±20 % so that many servers do not reconnect in step.
func TestBackoffJitter(t *testing.T) {
	m := New(nil, nil, nil, Options{MaxBackoff: 5 * time.Second})
	w := newWorker(m, Target{ID: "x"})
	for attempt, base := range []float64{1, 1.6, 2.56, 4.096, 5, 5, 5} {
		seen := map[time.Duration]bool{}
		for range 200 {
			d := w.backoff(attempt)
			lo, hi := time.Duration(base*0.8*float64(time.Second)), time.Duration(base*1.2*float64(time.Second))
			if d < lo || d > hi {
				t.Fatalf("attempt %d: %v outside %v..%v", attempt, d, lo, hi)
			}
			seen[d] = true
		}
		if len(seen) < 50 {
			t.Errorf("attempt %d: only %d different waits; jitter missing", attempt, len(seen))
		}
	}
}
