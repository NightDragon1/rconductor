// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
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

// TestRateLimitPruneOncePerInterval: a full table where no bucket is full
// again is scanned at most once per interval, not on every command; after
// the interval the next command scans again and drops what refilled
// (CMD-019, D-076).
func TestRateLimitPruneOncePerInterval(t *testing.T) {
	l := newLimiter()
	t0 := time.Unix(1_000_000, 0)
	for i := range maxBuckets {
		l.buckets[bucketKey{user: fmt.Sprintf("u%d", i)}] = &bucket{tokens: 0, last: t0, rate: 1, burst: 2}
	}
	for i := range 50 {
		l.take(fmt.Sprintf("n%d", i), "s", Limit{PerMinute: 60, Burst: 2}, t0.Add(time.Duration(i)*time.Millisecond))
	}
	if l.scans != 1 {
		t.Errorf("%d scans for 50 commands within the interval, want 1", l.scans)
	}
	later := t0.Add(pruneEvery + 2*time.Second) // every bucket is full again
	l.take("late", "s", Limit{PerMinute: 60, Burst: 2}, later)
	if l.scans != 2 || len(l.buckets) > 2 {
		t.Errorf("after the interval: %d scans, %d buckets", l.scans, len(l.buckets))
	}
}
