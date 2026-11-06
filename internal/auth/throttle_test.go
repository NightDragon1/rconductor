// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

func clientAt(ip string) Client {
	return Client{IP: netip.MustParseAddr(ip), UserAgent: "test", RequestID: "req"}
}

// TestLoginDelay covers the delay part of SEC-004: from the third attempt
// after consecutive failures the account answers later, 1 s, 2 s, 4 s.
func TestLoginDelay(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	// Spread over addresses so no lockout interferes.
	for i := range 6 {
		_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", clientAt(fmt.Sprintf("192.0.2.%d", i+1)))
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	if !slices.Equal(f.delays, want) {
		t.Errorf("delays = %v, want %v", f.delays, want)
	}
	// A successful login resets the counter.
	f.delays = nil
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("192.0.2.50")); err != nil {
		t.Fatal(err)
	}
	_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", clientAt("192.0.2.50"))
	if len(f.delays) != 1 { // the successful login itself was delayed once
		t.Errorf("delays after success = %v", f.delays)
	}
}

// TestLockoutPerAccountAndClient covers SEC-004: after five failures in a
// row an account is locked for that client address, audited and logged,
// while other addresses can still log in; the lock ends after 15 min.
func TestLockoutPerAccountAndClient(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	id := f.user(t, storetest.User{Pseudonym: "Mod"})
	attacker, owner := clientAt("198.51.100.7"), clientAt("192.0.2.10")

	for i := range DefaultMaxFailures {
		if _, _, err := f.svc.Login(ctx, "Mod", "wrong password!!", attacker); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if n := logtest.WaitCount(t, f.logs, `"event":"auth.lockout"`, 1); n != 1 {
		t.Errorf("auth.lockout = %d, want 1", n)
	}
	if f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'lockout' AND target_id = ? AND reason_code = 'rate_limited'`, id) != 1 {
		t.Error("lockout not audited")
	}

	// Locked for the attacker's address, even with the right password.
	_, _, err := f.svc.Login(ctx, "Mod", testPassword, attacker)
	var locked *LockedError
	if !errors.As(err, &locked) || locked.RetryAfter != DefaultLockout {
		t.Fatalf("locked login = %v", err)
	}
	if f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'login' AND reason_code = 'rate_limited'`) != 1 {
		t.Error("attempt during lockout not audited")
	}

	// The owner from another address is delayed but not locked out.
	f.delays = nil
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, owner); err != nil {
		t.Fatalf("owner locked out: %v", err)
	}
	if len(f.delays) != 1 || f.delays[0] != 4*time.Second {
		t.Errorf("owner delay = %v", f.delays)
	}

	// After the lockout the attacker's address may try again.
	for range DefaultMaxFailures {
		_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", attacker)
	}
	f.now = f.now.Add(DefaultLockout + time.Second)
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, attacker); err != nil {
		t.Errorf("after lockout: %v", err)
	}
}

func TestUnknownUserLocksLikeKnown(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	c := clientAt("198.51.100.7")
	for range DefaultMaxFailures {
		_, _, _ = f.svc.Login(ctx, "nobody", "whatever", c)
	}
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "nobody", "whatever", c); !errors.As(err, &locked) {
		t.Errorf("unknown user not locked: %v", err)
	}
	if strings.Contains(f.logs.String(), "nobody") {
		t.Error("unknown pseudonym in the log")
	}
}

func TestLockoutSettings(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE settings SET value = '2' WHERE key = ?`, SettingMaxFailures); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE settings SET value = '"1h"' WHERE key = ?`, SettingLockout); err != nil {
		t.Fatal(err)
	}
	f.user(t, storetest.User{Pseudonym: "Mod"})
	c := clientAt("198.51.100.7")
	_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", c)
	_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", c)
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, c); !errors.As(err, &locked) || locked.RetryAfter != time.Hour {
		t.Errorf("configured lockout ignored: %v", err)
	}
}

func TestThrottleMemoryBound(t *testing.T) {
	th := newThrottle()
	th.capacity = 100
	now := time.Now()
	for i := range 300 {
		th.fail(fmt.Sprintf("user%d", i), netip.MustParseAddr("192.0.2.1"), now.Add(time.Duration(i)*time.Millisecond), 5, time.Minute)
	}
	if n := th.size(); n > th.capacity {
		t.Errorf("tracked entries = %d, want at most %d", n, th.capacity)
	}
}

// TestEvictionRules covers Design 3.7: locked entries are never evicted,
// the oldest unlocked one goes first, and a table full of locks gives new
// attempts the highest delay.
func TestEvictionRules(t *testing.T) {
	now := time.Now()
	ip := func(i int) netip.Addr { return netip.MustParseAddr(fmt.Sprintf("198.51.100.%d", i)) }
	th := newThrottle()
	th.capacity = 4

	// Lock victim from address 1: one account entry and one locked pair.
	for i := range 2 {
		th.fail("victim", ip(1), now.Add(time.Duration(i)*time.Second), 2, time.Hour)
	}
	if _, locked := th.check("victim", ip(1), now.Add(3*time.Second), time.Hour); locked == 0 {
		t.Fatal("setup: no lockout")
	}
	// Two more entries fill the table; then a newer attempt evicts the
	// oldest unlocked entry, which is the victim's account counter.
	th.fail("a", ip(2), now.Add(4*time.Second), 5, time.Hour)
	th.fail("b", ip(3), now.Add(5*time.Second), 5, time.Hour)
	if th.size() != 4 {
		t.Fatalf("size = %d", th.size())
	}
	if _, ok := th.accounts["victim"]; ok {
		t.Error("oldest unlocked entry (victim's account counter) not evicted")
	}
	if _, locked := th.check("victim", ip(1), now.Add(6*time.Second), time.Hour); locked == 0 {
		t.Error("a locked entry was evicted")
	}

	// A table made only of lockouts takes nothing new and gives new
	// attempts the highest delay.
	full := newThrottle()
	full.capacity = 2
	for i := range 2 {
		full.pairs[pairKey{fmt.Sprintf("x%d", i), ip(10 + i)}] = &counter{last: now, lockedUntil: now.Add(time.Hour)}
	}
	full.fail("y", ip(20), now.Add(5*time.Second), 2, time.Hour) // no room
	full.fail("y", ip(20), now.Add(6*time.Second), 2, time.Hour)
	if full.size() != 2 {
		t.Errorf("full table grew to %d", full.size())
	}
	if d, _ := full.check("newcomer", ip(30), now.Add(7*time.Second), time.Hour); d != 4*time.Second {
		t.Errorf("delay with a table full of locks = %v, want 4s", d)
	}
}

// TestParallelBurstStaysUnderTheLimit covers T-002: attempts in parallel
// count before their result, so a burst gets no more guesses than the limit.
func TestParallelBurstStaysUnderTheLimit(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker := clientAt("192.0.2.77")
	var wg sync.WaitGroup
	var guessed atomic.Int32
	for range 40 {
		wg.Go(func() {
			if _, _, err := f.svc.Login(ctx, "Mod", "wrong password!!", attacker); errors.Is(err, ErrInvalidCredentials) {
				guessed.Add(1)
			}
		})
	}
	wg.Wait()
	if n := guessed.Load(); n > 5 {
		t.Errorf("%d guesses got through, the limit is 5", n)
	}
}

// TestIPv6NetworkIsOneClient covers T-002: rotating the address inside a /64
// does not start a new count.
func TestIPv6NetworkIsOneClient(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	for i := range 5 {
		_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", clientAt(fmt.Sprintf("2001:db8:1:2::%x", i+1)))
	}
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("2001:db8:1:2::ffff")); !errors.As(err, &locked) {
		t.Errorf("another address of the network = %v", err)
	}
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("2001:db8:1:3::1")); errors.As(err, &locked) {
		t.Errorf("another network is locked too: %v", err)
	}
}

// TestLockoutAuditsOnce covers T-016: a locked client repeats its attempts at
// HTTP speed without a session; that adds one audit row, not one per attempt.
func TestLockoutAuditsOnce(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker := clientAt("192.0.2.78")
	for range 5 {
		_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", attacker)
	}
	before := f.count(t, `SELECT count(*) FROM audit_log`)
	for range 50 {
		_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", attacker)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log`) - before; n != 1 {
		t.Errorf("%d audit rows for 50 refused attempts, want 1", n)
	}
}

// TestOverlongPasswordIsWrong covers T-025: a huge password fails like any
// other wrong one.
func TestOverlongPasswordIsWrong(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	if _, _, err := f.svc.Login(context.Background(), "Mod", strings.Repeat(testPassword, 1000), clientAt("192.0.2.79")); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v", err)
	}
}

// TestLongPseudonymStaysSmall covers T-025: the counters keep a bounded key,
// whatever name a client sends.
func TestLongPseudonymStaysSmall(t *testing.T) {
	if n := len(accountKey(strings.Repeat("x", 2<<20))); n > maxAccountKey {
		t.Errorf("key of %d bytes", n)
	}
}

// TestLowerLimitLocksNobodyOutForGood: failures from before a lower limit end
// with their window, so the right password works again.
func TestLowerLimitLocksNobodyOutForGood(t *testing.T) {
	tr := newThrottle()
	now := time.Now()
	ip := netip.MustParseAddr("192.0.2.80")
	for range 4 {
		tr.fail("mod", ip, now, 5, time.Minute)
	}
	if tr.reserve("mod", ip, now, time.Minute, 3) {
		t.Error("failures within the window do not count")
	}
	if !tr.reserve("mod", ip, now.Add(2*time.Minute), time.Minute, 3) {
		t.Error("old failures still block the login")
	}
}

// TestHangUpIsAudited covers SEC-004: a client that hangs up during the check
// still leaves its failed attempt in the audit log.
func TestHangUpIsAudited(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.svc.audit(ctx, audit.Entry{Kind: audit.KindAuth, Action: "login", Result: audit.ResultDenied, Reason: "unauthenticated"})
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'login'`); n != 1 {
		t.Errorf("%d audit rows after a hang-up", n)
	}
}
