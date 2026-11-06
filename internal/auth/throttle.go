// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"net/netip"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/netlimit"
)

// Settings keys of the login protection and their defaults (SEC-004,
// decision M1).
const (
	SettingMaxFailures       = "login.max_failures"
	SettingMaxClientFailures = "login.max_client_failures"
	SettingLockout           = "login.lockout_duration"

	DefaultMaxFailures       = 5
	DefaultMaxClientFailures = 50
	DefaultLockout           = 15 * time.Minute
)

// loginLimits are the settings of the login protection.
type loginLimits struct {
	maxFailures       int
	maxClientFailures int
	lockout           time.Duration
}

// maxTracked bounds the memory of the login protection against floods of
// made-up pseudonyms.
const maxTracked = 10000

// purgeGap spaces the scans for ended counters of a full table: an attempt
// that finds no room must not cost a walk over all counters each time.
const purgeGap = time.Second

// fullReportGap spaces the reports of a full table.
const fullReportGap = time.Minute

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
	failures int
	// first starts the window of a client counter.
	first       time.Time
	last        time.Time
	lockedUntil time.Time
	// reported is set once the first refused attempt of a lockout is in
	// the audit log; the following ones add no row (T-016).
	reported bool
}

// ended reports whether nothing depends on the counter any more: it is not
// locked and saw no failure for a whole window.
func (c *counter) ended(now time.Time, window time.Duration) bool {
	return !now.Before(c.lockedUntil) && now.Sub(c.last) >= window
}

// maxAccountKey is far above any pseudonym (Design 3.6 allows 64 characters).
const maxAccountKey = 256

type pairKey struct {
	account string
	ip      netip.Addr
}

// throttle counts login failures in memory: per account for the delay, per
// account and client address for the lockout, so that an attacker cannot lock
// a user out from everywhere, and per client address over all names, so that
// made-up names give no further attempts. Unknown pseudonyms are counted like
// real ones, so the behavior reveals nothing about which accounts exist.
// Counters end after the lockout duration without failures; a successful login
// ends only what its own client counted for that account. A restart forgets
// them.
type throttle struct {
	mu       sync.Mutex
	capacity int
	accounts map[string]*counter
	pairs    map[pairKey]*counter
	clients  map[netip.Addr]*counter
	// running counts attempts of a pair whose result is still open, so a
	// parallel burst cannot get past the limit before the first failure
	// is counted. clientRunning does the same per client.
	running       map[pairKey]int
	clientRunning map[netip.Addr]int
	// held counts the places reserved for the counters of running attempts.
	held      int
	nextPurge time.Time
	// full is set when an attempt finds no room; nextFullReport spaces the
	// reports of that.
	full           bool
	nextFullReport time.Time
}

func newThrottle() *throttle {
	return &throttle{
		capacity: maxTracked, accounts: map[string]*counter{}, pairs: map[pairKey]*counter{}, clients: map[netip.Addr]*counter{},
		running: map[pairKey]int{}, clientRunning: map[netip.Addr]int{},
	}
}

func (t *throttle) size() int { return len(t.accounts) + len(t.pairs) + len(t.clients) }

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

// check returns the delay before the password check and, if the pair or the
// client is locked, the remaining lockout.
func (t *throttle) check(account string, ip netip.Addr, now time.Time, window time.Duration) (delay, locked time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	client := netlimit.ClientKey(ip)
	if c := t.pairs[pairKey{account, client}]; c != nil && now.Before(c.lockedUntil) {
		locked = c.lockedUntil.Sub(now)
	}
	if c := t.clients[client]; c != nil && now.Before(c.lockedUntil) {
		locked = max(locked, c.lockedUntil.Sub(now))
	}
	if locked > 0 {
		return 0, locked
	}
	if c := t.accounts[account]; c != nil && now.Sub(c.last) < window {
		return delayFor(c.failures), 0
	}
	return 0, 0
}

// ticket is a reserved attempt.
type ticket struct {
	// places is the room held for the counters of the attempt.
	places int
}

// reserve counts an attempt as running. It refuses when the failures so far
// and the running attempts reach a limit. An attempt needs a place for each
// counter it may start. If the table has none it is refused: an attempt that
// could not be counted must not run at all (Design 3.7).
func (t *throttle) reserve(account string, ip netip.Addr, now time.Time, lim loginLimits) (ticket, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	client := netlimit.ClientKey(ip)
	key := pairKey{account, client}
	failures := 0
	if c := t.pairs[key]; c != nil {
		// A lockout that began since the check counts too.
		if now.Before(c.lockedUntil) {
			return ticket{}, false
		}
		// Failures end after the window, as in fail.
		if now.Sub(c.last) < lim.lockout {
			failures = c.failures
		}
	}
	if failures+t.running[key] >= lim.maxFailures {
		return ticket{}, false
	}
	failures = 0
	if c := t.clients[client]; c != nil {
		if now.Before(c.lockedUntil) {
			return ticket{}, false
		}
		if now.Sub(c.first) < lim.lockout {
			failures = c.failures
		}
	}
	if failures+t.clientRunning[client] >= lim.maxClientFailures {
		return ticket{}, false
	}
	tk := ticket{places: t.missing(account, key, now, lim.lockout)}
	if t.crowded(tk.places, now, lim.lockout) {
		t.full = true
		return ticket{}, false
	}
	t.held += tk.places
	t.running[key]++
	t.clientRunning[client]++
	return tk, true
}

// reportFull reports whether attempts were refused for lack of room since the
// last report. Reports are spaced: the refused can repeat at HTTP speed.
func (t *throttle) reportFull(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.full || now.Before(t.nextFullReport) {
		return false
	}
	t.full = false
	t.nextFullReport = now.Add(fullReportGap)
	return true
}

// release ends a reserved attempt.
func (t *throttle) release(account string, ip netip.Addr, tk ticket) {
	t.mu.Lock()
	defer t.mu.Unlock()
	client := netlimit.ClientKey(ip)
	key := pairKey{account, client}
	t.held -= tk.places
	if t.running[key]--; t.running[key] <= 0 {
		delete(t.running, key)
	}
	if t.clientRunning[client]--; t.clientRunning[client] <= 0 {
		delete(t.clientRunning, client)
	}
}

// firstRefused reports whether this is the first refused attempt of the
// current lockout. A locked client has one row for all names.
func (t *throttle) firstRefused(account string, ip netip.Addr, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	client := netlimit.ClientKey(ip)
	c := t.clients[client]
	if c == nil || !now.Before(c.lockedUntil) {
		c = t.pairs[pairKey{account, client}]
	}
	if c == nil || c.reported {
		return false
	}
	c.reported = true
	return true
}

// fail records a failure and reports which lockouts it starts. The client
// comes first: with little room left it is the counter that bounds the most.
func (t *throttle) fail(account string, ip netip.Addr, now time.Time, lim loginLimits, tk ticket) (pairLocked, clientLocked bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	places := tk.places
	fits := func() bool {
		if places > 0 {
			places--
			return true
		}
		return t.size()+t.held < t.capacity
	}
	bump := func(c *counter) *counter {
		if c == nil || now.Sub(c.last) >= lim.lockout {
			c = &counter{lockedUntil: lockedUntilOf(c)}
		}
		c.failures++
		c.last = now
		return c
	}
	client := netlimit.ClientKey(ip)
	if c, ok := t.clients[client]; ok || fits() {
		if c == nil || (now.Sub(c.first) >= lim.lockout && !now.Before(c.lockedUntil)) {
			c = &counter{first: now}
		}
		c.failures++
		c.last = now
		t.clients[client] = c
		if c.failures >= lim.maxClientFailures && !now.Before(c.lockedUntil) {
			c.lockedUntil = now.Add(lim.lockout)
			c.failures = 0
			c.reported = false
			clientLocked = true
		}
	}
	if _, ok := t.accounts[account]; ok || fits() {
		t.accounts[account] = bump(t.accounts[account])
	}
	key := pairKey{account, client}
	if _, ok := t.pairs[key]; !ok && !fits() {
		return false, clientLocked
	}
	p := bump(t.pairs[key])
	t.pairs[key] = p
	if p.failures >= lim.maxFailures && !now.Before(p.lockedUntil) {
		p.lockedUntil = now.Add(lim.lockout)
		p.failures = 0
		p.reported = false
		pairLocked = true
	}
	return pairLocked, clientLocked
}

func lockedUntilOf(c *counter) time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.lockedUntil
}

// success clears what this client counted for the account: its pair and its
// share of the account counter. Failures of other clients stay, so a login of
// the owner gives an attacker no new attempts (T-002). A share older than the
// window may not be in the account counter any more and stays there too. The
// client counter stays as well: anyone with an account of their own could
// otherwise reset it between guesses at other accounts.
func (t *throttle) success(account string, ip netip.Addr, now time.Time, window time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := pairKey{account, netlimit.ClientKey(ip)}
	p := t.pairs[key]
	delete(t.pairs, key)
	c := t.accounts[account]
	if p == nil || c == nil || now.Sub(p.last) >= window {
		return
	}
	if c.failures -= p.failures; c.failures <= 0 {
		delete(t.accounts, account)
	}
}

// missing returns how many counters an attempt may have to start.
func (t *throttle) missing(account string, key pairKey, now time.Time, window time.Duration) int {
	n := 0
	for _, c := range []*counter{t.accounts[account], t.pairs[key], t.clients[key.ip]} {
		if c == nil || c.ended(now, window) {
			n++
		}
	}
	return n
}

// crowded reports whether need more counters do not fit, after removing the
// ended ones. Nothing else ever leaves the table: a counter that is still
// running belongs to someone under watch, and evicting it would hand out new
// attempts (Design 3.7).
func (t *throttle) crowded(need int, now time.Time, window time.Duration) bool {
	if need == 0 || t.size()+t.held+need <= t.capacity {
		return false
	}
	if !now.Before(t.nextPurge) {
		t.nextPurge = now.Add(purgeGap)
		purge(t.accounts, now, window)
		purge(t.pairs, now, window)
		purge(t.clients, now, window)
	}
	return t.size()+t.held+need > t.capacity
}

func purge[K comparable](counters map[K]*counter, now time.Time, window time.Duration) {
	for k, c := range counters {
		if c.ended(now, window) {
			delete(counters, k)
		}
	}
}
