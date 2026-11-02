// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"log/slog"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
)

// ThrottleOptions bound connection attempts per source address (SEC-007,
// revision 2.3 candidate 2: 10 per minute, 5 at once).
type ThrottleOptions struct {
	PerMinute int
	Burst     int
	// MaxAddrs bounds the table; when full, the fullest bucket goes.
	MaxAddrs int
}

type bucket struct {
	tokens   float64
	last     time.Time
	loggedAt time.Time
}

type throttle struct {
	opts ThrottleOptions
	now  func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

func newThrottle(o ThrottleOptions) *throttle {
	if o.PerMinute <= 0 {
		o.PerMinute = 10
	}
	if o.Burst <= 0 {
		o.Burst = 5
	}
	if o.MaxAddrs <= 0 {
		o.MaxAddrs = 10000
	}
	return &throttle{opts: o, now: time.Now, buckets: map[string]*bucket{}}
}

// allow takes a token for addr.
func (t *throttle) allow(addr string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	b := t.buckets[addr]
	if b == nil {
		if len(t.buckets) >= t.opts.MaxAddrs {
			t.evict()
		}
		b = &bucket{tokens: float64(t.opts.Burst), last: now}
		t.buckets[addr] = b
	}
	b.tokens = min(float64(t.opts.Burst), b.tokens+now.Sub(b.last).Minutes()*float64(t.opts.PerMinute))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// evict drops the bucket with the most tokens, the address that is
// least throttled and loses nothing by starting fresh.
func (t *throttle) evict() {
	var best string
	bestTokens := -1.0
	for a, b := range t.buckets {
		if b.tokens > bestTokens {
			best, bestTokens = a, b.tokens
		}
	}
	delete(t.buckets, best)
}

// logOnce logs agent.limit at most once a minute per address, so that a
// flood does not flood the log.
func (t *throttle) logOnce(log *slog.Logger, addr string) {
	t.mu.Lock()
	b := t.buckets[addr]
	now := t.now()
	first := b != nil && now.Sub(b.loggedAt) >= time.Minute
	if first {
		b.loggedAt = now
	}
	t.mu.Unlock()
	if first {
		log.Warn("agent connection attempts throttled", logging.Event(logging.AgentLimit),
			slog.String(logging.KeyReason, "connection_rate"), slog.String("remote", addr))
	}
}
