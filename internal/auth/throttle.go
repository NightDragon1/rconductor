// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Settings keys of the login protection and their defaults (SEC-004,
// decision M1).
const (
	SettingMaxFailures = "login.max_failures"
	SettingLockout     = "login.lockout_duration"

	DefaultMaxFailures = 5
	DefaultLockout     = 15 * time.Minute
)

// maxTracked bounds the memory of the login protection against floods of
// made-up pseudonyms.
const maxTracked = 10000

// delayFor returns the wait before checking a password after n consecutive
// failures of the account: 1 s from the third on, then 2 s, then 4 s.
func delayFor(n int) time.Duration {
	switch {
	case n < 2:
		return 0
	case n == 2:
		return time.Second
	case n == 3:
		return 2 * time.Second
	default:
		return 4 * time.Second
	}
}

type counter struct {
	failures    int
	last        time.Time
	lockedUntil time.Time
}

type pairKey struct {
	account string
	ip      netip.Addr
}

// throttle counts consecutive login failures in memory: per account for
// the delay, per account and client address for the lockout, so that an
// attacker cannot lock a user out from everywhere. Unknown pseudonyms are
// counted like real ones, so the behaviour reveals nothing about which
// accounts exist. Counters end after the lockout duration without failures
// and with a successful login. A restart forgets them.
type throttle struct {
	mu       sync.Mutex
	accounts map[string]*counter
	pairs    map[pairKey]*counter
}

func newThrottle() *throttle {
	return &throttle{accounts: map[string]*counter{}, pairs: map[pairKey]*counter{}}
}

func accountKey(pseudonym string) string { return strings.ToLower(pseudonym) }

// check returns the delay before the password check and, if the pair is
// locked, the remaining lockout.
func (t *throttle) check(account string, ip netip.Addr, now time.Time, window time.Duration) (delay, locked time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.pairs[pairKey{account, ip}]; c != nil && now.Before(c.lockedUntil) {
		return 0, c.lockedUntil.Sub(now)
	}
	if c := t.accounts[account]; c != nil && now.Sub(c.last) < window {
		return delayFor(c.failures), 0
	}
	return 0, 0
}

// fail records a failure and reports whether it starts a lockout.
func (t *throttle) fail(account string, ip netip.Addr, now time.Time, maxFailures int, lockout time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(now, lockout)
	bump := func(c *counter) *counter {
		if c == nil || now.Sub(c.last) >= lockout {
			c = &counter{}
		}
		c.failures++
		c.last = now
		return c
	}
	t.accounts[account] = bump(t.accounts[account])
	p := bump(t.pairs[pairKey{account, ip}])
	t.pairs[pairKey{account, ip}] = p
	if p.failures >= maxFailures && !now.Before(p.lockedUntil) {
		p.lockedUntil = now.Add(lockout)
		p.failures = 0
		return true
	}
	return false
}

// success clears the counters of the account.
func (t *throttle) success(account string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accounts, account)
	for k := range t.pairs {
		if k.account == account {
			delete(t.pairs, k)
		}
	}
}

// prune drops counters that ended; if still too many remain, it drops
// arbitrary ones (map order), which only weakens protection, never locks
// anyone out.
func (t *throttle) prune(now time.Time, window time.Duration) {
	if len(t.accounts)+len(t.pairs) < maxTracked {
		return
	}
	for k, c := range t.accounts {
		if now.Sub(c.last) >= window {
			delete(t.accounts, k)
		}
	}
	for k, c := range t.pairs {
		if now.Sub(c.last) >= window && !now.Before(c.lockedUntil) {
			delete(t.pairs, k)
		}
	}
	for k := range t.accounts {
		if len(t.accounts)+len(t.pairs) < maxTracked/2 {
			break
		}
		delete(t.accounts, k)
	}
	for k, c := range t.pairs {
		if len(t.accounts)+len(t.pairs) < maxTracked/2 {
			break
		}
		if !now.Before(c.lockedUntil) {
			delete(t.pairs, k)
		}
	}
}
