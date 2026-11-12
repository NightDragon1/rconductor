// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"fmt"
	"testing"
	"time"
)

// TestRateLimitPruneByOwnLimitC15: pruning a full limiter judges each bucket
// by its own rate and burst; a bucket of a slower role that has not refilled
// keeps its count when a faster role triggers the pruning (CMD-019, D-076).
func TestRateLimitPruneByOwnLimitC15(t *testing.T) {
	l := newLimiter()
	t0 := time.Unix(1_000_000, 0)
	sup := DefaultLimits["supporter"]
	adm := DefaultLimits["rcon_admin"]
	for i := 0; len(l.buckets) < maxBuckets-1; i++ {
		l.take(fmt.Sprintf("u%d", i), "s", sup, t0)
	}
	for range sup.Burst {
		if ok, _ := l.take("victim", "srv", sup, t0); !ok {
			t.Fatal("burst refused")
		}
	}
	if ok, _ := l.take("victim", "srv", sup, t0); ok {
		t.Fatal("over the burst")
	}
	// 10 s later a command of the faster role prunes the table. By its own
	// rate (10 a minute) the spent bucket holds 1.67 tokens: one command.
	t1 := t0.Add(10 * time.Second)
	l.take("admin", "other", adm, t1)
	if _, kept := l.buckets[bucketKey{"victim", "srv"}]; !kept {
		t.Error("spent bucket of the slower role was dropped")
	}
	got := 0
	for {
		ok, _ := l.take("victim", "srv", sup, t1)
		if !ok {
			break
		}
		got++
	}
	if got != 1 {
		t.Errorf("commands allowed 10 s after the burst = %d, want 1", got)
	}
	// The buckets used once (2 of 3 tokens left) are full again by now, by
	// their own rate, and went.
	if n := len(l.buckets); n > 3 {
		t.Errorf("buckets after pruning = %d, want only the spent ones", n)
	}
	// Pruning never empties a table whose buckets all still count.
	for i := 0; len(l.buckets) < maxBuckets; i++ {
		for range sup.Burst {
			l.take(fmt.Sprintf("w%d", i), "s", sup, t1)
		}
	}
	l.take("admin", "third", adm, t1)
	if n := len(l.buckets); n < maxBuckets {
		t.Errorf("buckets after pruning a table of spent buckets = %d, want all %d kept", n, maxBuckets)
	}
}
