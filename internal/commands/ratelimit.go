// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"sync"
	"time"
)

// Limit is the token bucket of one role (Design 3.4): PerMinute tokens a
// minute, at most Burst at once.
type Limit struct {
	PerMinute int
	Burst     int
}

// DefaultLimits are the defaults of Design 3.4.
var DefaultLimits = map[string]Limit{
	"rcon_admin":   {PerMinute: 120, Burst: 20},
	"server_admin": {PerMinute: 60, Burst: 10},
	"moderator":    {PerMinute: 20, Burst: 5},
	"supporter":    {PerMinute: 10, Burst: 3},
}

type bucket struct {
	tokens float64
	last   time.Time
}

type bucketKey struct{ user, server string }

// maxBuckets bounds the memory; buckets that are full again are dropped.
const maxBuckets = 10000

// limiter holds one bucket per user and server (CMD-019).
type limiter struct {
	mu      sync.Mutex
	buckets map[bucketKey]*bucket
}

func newLimiter() *limiter { return &limiter{buckets: map[bucketKey]*bucket{}} }

// take spends one token; if none is left it returns the wait until the
// next one.
func (l *limiter) take(user, server string, lim Limit, now time.Time) (bool, time.Duration) {
	if lim.PerMinute <= 0 || lim.Burst <= 0 {
		return false, time.Minute
	}
	rate := float64(lim.PerMinute) / 60 // tokens per second
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) >= maxBuckets {
		l.prune(now, rate)
	}
	k := bucketKey{user, server}
	b := l.buckets[k]
	if b == nil {
		b = &bucket{tokens: float64(lim.Burst), last: now}
		l.buckets[k] = b
	}
	b.tokens = min(float64(lim.Burst), b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / rate * float64(time.Second))
}

func (l *limiter) prune(now time.Time, rate float64) {
	for k, b := range l.buckets {
		if now.Sub(b.last).Seconds()*rate >= float64(maxBuckets) || now.Sub(b.last) > time.Hour {
			delete(l.buckets, k)
		}
	}
}
