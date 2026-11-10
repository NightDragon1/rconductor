// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"strconv"
	"testing"
	"time"
)

// TestOrderLimiterFullTableKeepsCountsD076: a full order limiter is never
// cleared as a whole: a spent bucket keeps its count, a new container is
// refused while no bucket is full again, and room comes back as buckets
// refill (AGT-003, D-076).
func TestOrderLimiterFullTableKeepsCountsD076(t *testing.T) {
	l := newOrderLimiter(10, 3)
	now := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return now }
	for i := range maxOrderBuckets - 1 {
		for range 3 {
			l.allow("c" + strconv.Itoa(i))
		}
	}
	for range 3 {
		if !l.allow("spent") {
			t.Fatal("refused within the burst")
		}
	}
	now = now.Add(time.Second)
	if l.allow("new") {
		t.Error("new container on a full table was let through")
	}
	if l.allow("spent") {
		t.Error("spent bucket lost its count on a full table")
	}
	// 18 s refill all buckets (10 a minute, burst 3).
	now = now.Add(18 * time.Second)
	if !l.allow("new") {
		t.Error("no room after the buckets refilled")
	}
}
