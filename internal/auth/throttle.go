// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"net/netip"
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

// maxDelayFailures is a failure count that yields the highest delay.
const maxDelayFailures = 4

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
	// reported is set once the first refused attempt of a lockout is in
	// the audit log; the following ones add no row (T-016).
	reported bool
}

// maxAccountKey is far above any pseudonym (Design 3.6 allows 64 characters).
const maxAccountKey = 256

type pairKey struct {
	account string
	ip      netip.Addr
}

// clientKey is the address the lockout counts: an IPv6 client usually owns a
// whole /64, so rotating inside it must not start a new count.
func clientKey(ip netip.Addr) netip.Addr {
	ip = ip.Unmap()
	if ip.Is6() {
		if p, err := ip.Prefix(64); err == nil {
			return p.Addr()
		}
	}
	return ip
}

// throttle counts consecutive login failures in memory: per account for
// the delay, per account and client address for the lockout, so that an
// attacker cannot lock a user out from everywhere. Unknown pseudonyms are
// counted like real ones, so the behavior reveals nothing about which
// accounts exist. Counters end after the lockout duration without failures
// and with a successful login. A restart forgets them.
type throttle struct {
	mu       sync.Mutex
	capacity int
	accounts map[string]*counter
	pairs    map[pairKey]*counter
	// running counts attempts of a pair whose result is still open, so a
	// parallel burst cannot get past the limit before the first failure
	// is counted.
	running map[pairKey]int
}

func newThrottle() *throttle {
	return &throttle{capacity: maxTracked, accounts: map[string]*counter{}, pairs: map[pairKey]*counter{}, running: map[pairKey]int{}}
}

func (t *throttle) size() int { return len(t.accounts) + len(t.pairs) }

// accountKey is what the counters remember of a pseudonym. It is a copy of
// bounded size: the name comes from anyone who tries to sign in, and a slice
// would keep the whole string in memory (T-025). It folds ASCII only, like the
// NOCASE lookup of the user, so two accounts never share a counter.
func accountKey(pseudonym string) string {
	if len(pseudonym) > maxAccountKey {
		pseudonym = pseudonym[:maxAccountKey]
	}
	key := []byte(pseudonym)
	for i, c := range key {
		if 'A' <= c && c <= 'Z' {
			key[i] = c + 'a' - 'A'
		}
	}
	return string(key)
}

// check returns the delay before the password check and, if the pair is
// locked, the remaining lockout.
func (t *throttle) check(account string, ip netip.Addr, now time.Time, window time.Duration) (delay, locked time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.pairs[pairKey{account, clientKey(ip)}]; c != nil && now.Before(c.lockedUntil) {
		return 0, c.lockedUntil.Sub(now)
	}
	if c := t.accounts[account]; c != nil && now.Sub(c.last) < window {
		return delayFor(c.failures), 0
	}
	if _, tracked := t.accounts[account]; !tracked && t.size() >= t.capacity && !t.hasEvictable(now) {
		// The table is full of lockouts: untracked attempts get the
		// highest delay instead of no protection (Design 3.7).
		return delayFor(maxDelayFailures), 0
	}
	return 0, 0
}

// reserve counts an attempt as running. It refuses when the failures so far
// and the running attempts already reach the limit.
func (t *throttle) reserve(account string, ip netip.Addr, now time.Time, window time.Duration, maxFailures int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := pairKey{account, clientKey(ip)}
	failures := 0
	if c := t.pairs[key]; c != nil {
		// A lockout that began since the check counts too.
		if now.Before(c.lockedUntil) {
			return false
		}
		// Failures end after the window, as in fail.
		if now.Sub(c.last) < window {
			failures = c.failures
		}
	}
	if failures+t.running[key] >= maxFailures {
		return false
	}
	t.running[key]++
	return true
}

// release ends a reserved attempt.
func (t *throttle) release(account string, ip netip.Addr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := pairKey{account, clientKey(ip)}
	if t.running[key]--; t.running[key] <= 0 {
		delete(t.running, key)
	}
}

// firstRefused reports whether this is the first refused attempt of the
// current lockout of the pair.
func (t *throttle) firstRefused(account string, ip netip.Addr) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.pairs[pairKey{account, clientKey(ip)}]
	if c == nil || c.reported {
		return false
	}
	c.reported = true
	return true
}

// fail records a failure and reports whether it starts a lockout.
func (t *throttle) fail(account string, ip netip.Addr, now time.Time, maxFailures int, lockout time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	bump := func(c *counter) *counter {
		if c == nil || now.Sub(c.last) >= lockout {
			c = &counter{lockedUntil: lockedUntilOf(c)}
		}
		c.failures++
		c.last = now
		return c
	}
	if _, ok := t.accounts[account]; ok || t.makeRoom(now, lockout) {
		t.accounts[account] = bump(t.accounts[account])
	}
	key := pairKey{account, clientKey(ip)}
	if _, ok := t.pairs[key]; !ok && !t.makeRoom(now, lockout) {
		return false
	}
	p := bump(t.pairs[key])
	t.pairs[key] = p
	if p.failures >= maxFailures && !now.Before(p.lockedUntil) {
		p.lockedUntil = now.Add(lockout)
		p.failures = 0
		p.reported = false
		return true
	}
	return false
}

func lockedUntilOf(c *counter) time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.lockedUntil
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

// makeRoom frees one place if the table is full and reports whether a new
// entry fits. Locked entries are never evicted; first an ended counter
// goes, else the oldest unlocked one (Design 3.7).
func (t *throttle) makeRoom(now time.Time, window time.Duration) bool {
	if t.size() < t.capacity {
		return true
	}
	var oldest time.Time
	var dropAccount string
	var dropPair *pairKey
	found := false
	consider := func(c *counter, onAccount string, onPair *pairKey) {
		if now.Before(c.lockedUntil) {
			return
		}
		if !found || c.last.Before(oldest) {
			oldest, dropAccount, dropPair, found = c.last, onAccount, onPair, true
		}
	}
	for k, c := range t.accounts {
		consider(c, k, nil)
	}
	for k, c := range t.pairs {
		consider(c, "", &k)
	}
	if !found {
		return false
	}
	if dropPair != nil {
		delete(t.pairs, *dropPair)
	} else {
		delete(t.accounts, dropAccount)
	}
	return true
}

// hasEvictable reports whether any entry is not locked.
func (t *throttle) hasEvictable(now time.Time) bool {
	if len(t.accounts) > 0 {
		return true // account counters are never locked
	}
	for _, c := range t.pairs {
		if !now.Before(c.lockedUntil) {
			return true
		}
	}
	return false
}
