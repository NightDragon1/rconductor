// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"runtime"
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

func limits(maxFailures int, lockout time.Duration) loginLimits {
	return loginLimits{maxFailures: maxFailures, maxClientFailures: DefaultMaxClientFailures, lockout: lockout}
}

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
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if !slices.Equal(f.delays, want) {
		t.Errorf("delays = %v, want %v", f.delays, want)
	}
	// A login from another client leaves the delay in place (Design 3.7).
	f.delays = nil
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("192.0.2.50")); err != nil {
		t.Fatal(err)
	}
	_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", clientAt("192.0.2.50"))
	if !slices.Equal(f.delays, []time.Duration{16 * time.Second, 16 * time.Second}) {
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
	if len(f.delays) != 1 || f.delays[0] != 8*time.Second {
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
		th.fail(fmt.Sprintf("user%d", i), netip.MustParseAddr("192.0.2.1"), now.Add(time.Duration(i)*time.Millisecond), limits(5, time.Minute), ticket{})
	}
	if n := th.size(); n > th.capacity {
		t.Errorf("tracked entries = %d, want at most %d", n, th.capacity)
	}
}

// TestEvictionRules covers Design 3.7: only ended counters leave a full
// table. A counter that still runs stays, locked or not.
func TestEvictionRules(t *testing.T) {
	now := time.Now()
	ip := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}) }
	th := newThrottle()
	th.capacity = 6
	lim := limits(2, time.Hour)
	th.fail("old", ip(1), now, lim, ticket{})
	th.fail("live", ip(2), now.Add(30*time.Minute), lim, ticket{})
	if th.size() != 6 {
		t.Fatalf("size = %d", th.size())
	}

	// The window of the first three counters has ended, the others run.
	later := now.Add(time.Hour)
	tk, ok := th.reserve("new", ip(3), later, lim)
	if !ok || tk.places != 3 {
		t.Fatalf("no room although counters ended: %+v %v", tk, ok)
	}
	if _, kept := th.accounts["old"]; kept {
		t.Error("ended counter kept")
	}
	if th.accounts["live"] == nil || th.pairs[pairKey{"live", ip(2)}] == nil || th.clients[ip(2)] == nil {
		t.Error("a running counter was evicted")
	}
}

// TestR1B2FloodKeepsRunningCounters covers T-002: the flood of made-up names from
// other addresses cannot wipe the failures of an attacker.
func TestR1B2FloodKeepsRunningCounters(t *testing.T) {
	f := newSessionFixture(t, true)
	f.svc.minTracked = 1
	if _, err := f.st.DB.ExecContext(context.Background(), `UPDATE settings SET value = '60' WHERE key = ?`, SettingMaxTracked); err != nil {
		t.Fatal(err)
	}
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker := clientAt("192.0.2.90")
	for range 4 {
		_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", attacker)
	}
	for i := range 200 {
		c := clientAt(fmt.Sprintf("198.51.100.%d", i))
		_, _, _ = f.svc.Login(ctx, fmt.Sprintf("made-up-%d", i), "wrong password!!", c)
	}
	if n := f.svc.throttle.size(); n > 60 {
		t.Errorf("%d counters, the table holds 60", n)
	}
	if c := f.svc.throttle.pairs[pairKey{"mod", attacker.IP}]; c == nil || c.failures != 4 {
		t.Fatalf("counter of the attacker after the flood = %+v", c)
	}
	_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", attacker)
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, attacker); !errors.As(err, &locked) {
		t.Errorf("sixth attempt after the flood = %v", err)
	}
}

// fullTable returns a throttle full of running counters of other clients.
func fullTable(now time.Time) *throttle {
	th := newThrottle()
	th.capacity = 9
	for i := range 3 {
		th.fail(fmt.Sprintf("x%d", i), netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}), now, limits(5, time.Hour), ticket{})
	}
	return th
}

// attempt runs one attempt of the line to its end, with a failure.
func attempt(t *testing.T, th *throttle, name string, ip netip.Addr, at time.Time, lim loginLimits) bool {
	t.Helper()
	tk, ok := th.reserve(name, ip, at, lim)
	if !ok {
		return false
	}
	if tk.place == nil {
		t.Fatal("no place in line on a full table")
	}
	now := func() time.Time { return at }
	sleep := func(context.Context, time.Duration) error { return nil }
	if _, turn, err := th.wait(t.Context(), tk.place, name, sleep, now, lim.lockout, overflowGap); err != nil || !turn {
		t.Fatalf("wait = %v, %v", turn, err)
	}
	th.fail(name, ip, at, lim, tk)
	th.release(name, ip, tk)
	return true
}

// TestL1FullTableCountsItsLine covers Design 3.7: an attempt without room for
// its counters is counted on the sketch, and an attacker gets no attempts beyond
// the limits there either, whichever account it goes for.
func TestL1FullTableCountsItsLine(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := netip.AddrFrom4([4]byte{203, 0, 113, 1})
	attempts := 0
	for i := range 100 {
		name := "victim"
		if i%2 == 1 {
			name = fmt.Sprintf("made-up-%d", i)
		}
		if attempt(t, th, name, ip, now.Add(time.Duration(i)*time.Second), lim) {
			attempts++
		}
	}
	if attempts != 5 {
		t.Errorf("%d attempts from one client on a full table, the limit is 5", attempts)
	}
	if th.size() != 9 {
		t.Errorf("full table grew to %d", th.size())
	}
	// The window ends there as well.
	if _, ok := th.reserve("victim", ip, now.Add(2*time.Hour), lim); !ok {
		t.Error("no attempt after the window")
	}
}

// TestR3B4FailureWithoutRoomStaysCounted: an attempt found its counters, they
// ended while it ran and their places went to others. Its failure goes to the
// sketch instead of nowhere.
func TestR3B4FailureWithoutRoomStaysCounted(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := netip.AddrFrom4([4]byte{203, 0, 113, 7})
	th.fail("gone", ip, now, lim, ticket{})
	if n := th.overflow.count(pairName(pairKey{"gone", ip}), now, time.Hour); n != 1 {
		t.Errorf("pair on the sketch = %d", n)
	}
	if n := th.overflow.count(clientName(ip), now, time.Hour); n != 1 {
		t.Errorf("client on the sketch = %d", n)
	}
	if n := th.overflow.count(accountName("gone"), now, time.Hour); n != 1 {
		t.Errorf("account on the sketch = %d", n)
	}
}

// TestR4B2CountedAttackerFallsBehind: what a client did in the table counts in
// the line as well. The addresses that filled the table have no attempts left
// there and cannot keep anyone out.
func TestR4B2CountedAttackerFallsBehind(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	x := netip.AddrFrom4([4]byte{198, 51, 100, 0})
	if _, ok := th.reserve("new", x, now, lim); !ok {
		t.Fatal("one failure in the table keeps a client out of the line")
	}
	th.clients[x].failures = 5
	if _, ok := th.reserve("other", x, now, lim); ok {
		t.Error("a client with 5 failures in the table got a place in the line")
	}
}

// TestR4B3RunningAttemptsCountInLine: an attempt that left the line but has no
// result yet still counts, so its client cannot queue again meanwhile.
func TestR4B3RunningAttemptsCountInLine(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := netip.AddrFrom4([4]byte{203, 0, 113, 8})
	sleep := func(context.Context, time.Duration) error { return nil }
	clock := func() time.Time { return now }
	running := 0
	for i := range 50 {
		name := fmt.Sprintf("n%d", i)
		tk, ok := th.reserve(name, ip, now, lim)
		if !ok {
			continue
		}
		if _, turn, err := th.wait(t.Context(), tk.place, name, sleep, clock, time.Hour, overflowGap); err != nil || !turn {
			t.Fatalf("wait = %v, %v", turn, err)
		}
		running++ // no result yet: neither fail nor release
	}
	if running != 5 {
		t.Errorf("%d attempts of one client running at once, the limit is 5", running)
	}
}

// TestR4B4SketchCountsWithRoom: failures from the line go on counting when the
// table has room again, so its end gives no new attempts.
func TestR4B4SketchCountsWithRoom(t *testing.T) {
	now := time.Now()
	th := newThrottle()
	th.capacity = 100
	lim := limits(5, time.Hour)
	ip := netip.AddrFrom4([4]byte{203, 0, 113, 9})
	for range 5 {
		th.overflow.add(pairName(pairKey{"victim", ip}), now, time.Hour)
	}
	if _, ok := th.reserve("victim", ip, now, lim); ok {
		t.Error("the pair got new attempts with room in the table")
	}
	for range DefaultMaxClientFailures {
		th.overflow.add(clientName(ip), now, time.Hour)
	}
	if _, ok := th.reserve("other", ip, now, lim); ok {
		t.Error("the client got new attempts with room in the table")
	}
}

// TestR4B3PairOnTheLine is the pair part of B-3: four failures, then attempts
// at once, stay within the limit of the pair.
func TestR4B3PairOnTheLine(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := netip.AddrFrom4([4]byte{203, 0, 113, 8})
	sleep := func(context.Context, time.Duration) error { return nil }
	clock := func() time.Time { return now }
	total := 0
	for range 4 {
		if attempt(t, th, "victim", ip, now, lim) {
			total++
		}
	}
	for range 10 {
		tk, ok := th.reserve("victim", ip, now, lim)
		if !ok {
			continue
		}
		if _, turn, err := th.wait(t.Context(), tk.place, "victim", sleep, clock, time.Hour, overflowGap); err != nil || !turn {
			t.Fatalf("wait = %v, %v", turn, err)
		}
		total++
	}
	if total > 5 {
		t.Errorf("%d attempts on one pair, the limit is 5", total)
	}
}

// TestR4B4NoNewAttemptsOnceThereIsRoom is the way of the review: a pair used up
// its attempts in the line, then the table gets room.
func TestR4B4NoNewAttemptsOnceThereIsRoom(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := netip.AddrFrom4([4]byte{203, 0, 113, 9})
	for attempt(t, th, "victim", ip, now, lim) {
	}
	lim.maxTracked = 100
	if _, ok := th.reserve("victim", ip, now, lim); ok {
		t.Error("new attempts for the pair once the table has room")
	}
}

// TestR4B5ReportDoesNotHoldTheLine: the audit entry about the full table comes
// after the turn. With the database locked the first client hangs up, and the
// turn must not stay taken for the time the entry waits.
func TestR4B5ReportDoesNotHoldTheLine(t *testing.T) {
	f := fullFixture(t)
	f.svc.now = time.Now
	f.svc.throttle = fullTable(time.Now())
	ctx := context.Background()
	tx, err := f.st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET key = key`); err != nil {
		t.Fatal(err)
	}
	first, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = f.svc.Login(first, "Mod", testPassword, clientAt("203.0.113.1"))
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(300 * time.Millisecond)
	f.svc.throttle.mu.Lock()
	taken := f.svc.throttle.turnTaken
	f.svc.throttle.mu.Unlock()
	_ = tx.Rollback()
	<-done
	if taken {
		t.Error("the turn stayed taken while the report waits for the database")
	}
}

// TestSketchNeedsTwoCollisions: a name counts as the lower of its two slots.
func TestSketchNeedsTwoCollisions(t *testing.T) {
	now := time.Now()
	k := newSketch()
	*k.rows[0].slot("owner") = sketchSlot{failures: 50, epoch: epochOf(now, time.Hour)}
	if n := k.count("owner", now, time.Hour); n != 0 {
		t.Errorf("one collision counts %d", n)
	}
}

// TestCountedClientsKeepGoing: who is already counted does not queue.
func TestCountedClientsKeepGoing(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	x := netip.AddrFrom4([4]byte{198, 51, 100, 0})
	tk, ok := th.reserve("x0", x, now, lim)
	if !ok || tk.place != nil {
		t.Fatalf("counted client = %+v %v", tk, ok)
	}
	th.fail("x0", x, now, lim, tk)
	if c := th.pairs[pairKey{"x0", x}]; c.failures != 2 {
		t.Errorf("failures = %d", c.failures)
	}
}

// TestLineIsBounded covers a burst: one place per client, overflowLine in
// total. A client without failures pushes out one with failures, never the
// other way round, and it goes first.
func TestLineIsBounded(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{203, 0, byte(i / 250), byte(i % 250)}) }
	th.turnTaken = true // someone has the turn, all others wait
	var places []*waiter
	for i := range overflowLine {
		th.overflow.add(clientName(ip(i)), now, time.Hour)
		tk, ok := th.reserve("a", ip(i), now, lim)
		if !ok {
			t.Fatalf("place %d refused", i)
		}
		places = append(places, tk.place)
	}
	if _, ok := th.reserve("b", ip(0), now, lim); ok {
		t.Error("second place for one client")
	}
	th.overflow.add(clientName(ip(500)), now, time.Hour)
	if _, ok := th.reserve("a", ip(500), now, lim); ok {
		t.Fatal("line over its bound")
	}
	owner, ok := th.reserve("mod", ip(900), now, lim)
	if !ok {
		t.Fatal("client without failures found no place")
	}
	if n := len(th.line); n != overflowLine {
		t.Errorf("line has %d", n)
	}
	pushed := 0
	for _, w := range places {
		select {
		case turn := <-w.answer:
			if turn {
				t.Error("turn for a client with failures")
			}
			pushed++
		default:
		}
	}
	if pushed != 1 {
		t.Errorf("%d clients pushed out, want 1", pushed)
	}
	th.turnTaken = false
	th.nextTurn()
	if !<-owner.place.answer {
		t.Error("client without failures did not go first")
	}
}

// fullFixture is a service whose table is full of counters of other clients.
func fullFixture(t *testing.T) *sessionFixture {
	t.Helper()
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	f.svc.throttle = fullTable(f.now)
	f.svc.minTracked = 1
	if _, err := f.st.DB.ExecContext(context.Background(), `UPDATE settings SET value = '9' WHERE key = ?`, SettingMaxTracked); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestR12FullTableLetsTheOwnerIn is the point of the line: with the table full of
// other clients the right password works after a bounded wait, and one report
// per minute at most goes to the audit log.
func TestR12FullTableLetsTheOwnerIn(t *testing.T) {
	f := fullFixture(t)
	ctx := context.Background()
	for i := range 3 {
		if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt(fmt.Sprintf("203.0.113.%d", i))); err != nil {
			t.Fatalf("login on a full table: %v", err)
		}
	}
	if !slices.Equal(f.delays, []time.Duration{overflowGap, overflowGap, overflowGap}) {
		t.Errorf("delays = %v", f.delays)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'lockout'`); n != 1 {
		t.Errorf("%d reports of the full table, want 1", n)
	}
	logtest.Wait(t, f.logs, `"event":"auth.lockout"`, `"scope":"table"`)
}

// TestOwnerStaysAfterAnAttackOnTheLine: failures of an attacker in the line do
// not lock the owner out, they only slow the account down (Design 3.7).
func TestOwnerStaysAfterAnAttackOnTheLine(t *testing.T) {
	f := fullFixture(t)
	ctx := context.Background()
	var locked *LockedError
	guessed := 0
	for range 20 {
		_, _, err := f.svc.Login(ctx, "Mod", "wrong password!!", clientAt("203.0.113.50"))
		if errors.Is(err, ErrInvalidCredentials) {
			guessed++
		} else if !errors.As(err, &locked) {
			t.Fatal(err)
		}
	}
	if guessed != 5 {
		t.Errorf("%d guesses, the limit is 5", guessed)
	}
	f.delaysMu.Lock()
	f.delays = nil
	f.delaysMu.Unlock()
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("203.0.113.51")); err != nil {
		t.Fatalf("owner after the attack: %v", err)
	}
	if !slices.Equal(f.delays, []time.Duration{overflowGap, 8 * time.Second}) {
		t.Errorf("delays of the owner = %v, want the line and the delay of the account", f.delays)
	}
}

// TestHangUpLeavesTheLine: a client that gives up during its turn hands the
// turn on.
func TestHangUpLeavesTheLine(t *testing.T) {
	f := fullFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.svc.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	c := clientAt("203.0.113.60")
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("hang-up = %v", err)
	}
	if n := len(f.svc.throttle.line); n != 0 || f.svc.throttle.turnTaken {
		t.Errorf("line %d, turn taken %v", n, f.svc.throttle.turnTaken)
	}
}

// TestHangUpInLine: a client that gives up while waiting frees its place, and
// the turn is not lost if it just got it.
func TestHangUpInLine(t *testing.T) {
	now := time.Now()
	th := fullTable(now)
	lim := limits(5, time.Hour)
	ip := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{203, 0, 113, byte(i)}) }
	first, _ := th.reserve("a", ip(1), now, lim)
	second, _ := th.reserve("b", ip(2), now, lim)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sleep := func(context.Context, time.Duration) error { return nil }
	clock := func() time.Time { return now }
	if _, _, err := th.wait(ctx, second.place, "b", sleep, clock, time.Hour, overflowGap); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait in line = %v", err)
	}
	if n := len(th.line); n != 0 {
		t.Errorf("%d places left", n)
	}
	// The first has the turn and hangs up; either way it hands the turn on.
	_, _, _ = th.wait(ctx, first.place, "a", sleep, clock, time.Hour, overflowGap)
	if th.turnTaken {
		t.Error("the turn stayed taken")
	}
}

// TestMaxTrackedSetting covers the setting of the table size.
func TestMaxTrackedSetting(t *testing.T) {
	f := newSessionFixture(t, true)
	ctx := context.Background()
	if got := f.svc.loginPolicy(ctx).maxTracked; got != DefaultMaxTracked {
		t.Errorf("default = %d", got)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE settings SET value = '2000' WHERE key = ?`, SettingMaxTracked); err != nil {
		t.Fatal(err)
	}
	_, _, _ = f.svc.Login(ctx, "x", "wrong password!!", clientAt("192.0.2.1"))
	if got := f.svc.throttle.capacity; got != 2000 {
		t.Errorf("capacity = %d", got)
	}
}

// TestHeldPlacesFitTheirCounters covers the order of a burst: attempts that
// run in parallel each hold their places, so all their failures are counted
// and the table stays within its bound.
func TestHeldPlacesFitTheirCounters(t *testing.T) {
	now := time.Now()
	th := newThrottle()
	th.capacity = 9
	lim := limits(5, time.Hour)
	ip := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{203, 0, 113, byte(i)}) }
	var held []ticket
	for i := range 3 {
		tk, ok := th.reserve(fmt.Sprintf("u%d", i), ip(i), now, lim)
		if !ok || tk.places != 3 {
			t.Fatalf("attempt %d = %+v %v", i, tk, ok)
		}
		held = append(held, tk)
	}
	over, ok := th.reserve("u9", ip(9), now, lim)
	if !ok || over.place == nil {
		t.Fatalf("fourth attempt found room that is held: %+v %v", over, ok)
	}
	// The attempt of the line ends first and must not take a held place.
	th.fail("u9", ip(9), now, lim, over)
	th.release("u9", ip(9), over)
	for i, tk := range held {
		th.fail(fmt.Sprintf("u%d", i), ip(i), now, lim, tk)
		th.release(fmt.Sprintf("u%d", i), ip(i), tk)
	}
	if th.size() != 9 || th.held != 0 {
		t.Errorf("size = %d, held = %d, want 9 and 0", th.size(), th.held)
	}
	for i := range 3 {
		if th.pairs[pairKey{fmt.Sprintf("u%d", i), ip(i)}] == nil {
			t.Errorf("failure of attempt %d not counted", i)
		}
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

// TestLongPseudonymStaysSmall covers T-025: the counters keep a small copy of
// a name, not the string the name was cut from.
func TestLongPseudonymStaysSmall(t *testing.T) {
	tr := newThrottle()
	ip := netip.MustParseAddr("192.0.2.81")
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range 50 {
		name := fmt.Sprintf("%d", i) + strings.Repeat("x", 1<<20)
		tr.fail(accountKey(name), ip, time.Now(), limits(5, time.Minute), ticket{})
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 4<<20 {
		t.Errorf("50 names of 1 MiB keep %d bytes", grown)
	}
	runtime.KeepAlive(tr)
}

// TestAccountsNeverShareACounter: the key folds like the lookup, so a similar
// account cannot clear the counters of another (T-002).
func TestAccountsNeverShareACounter(t *testing.T) {
	for _, pair := range [][2]string{{"Kai", "\u212Aai"}, {"Müller", "MÜLLER"}, {"admin", "adm\u0130n"}} {
		if accountKey(pair[0]) == accountKey(pair[1]) {
			t.Errorf("%q and %q share a key", pair[0], pair[1])
		}
	}
	if accountKey("MoD") != accountKey("mod") {
		t.Error("ASCII case must fold, as in the lookup")
	}
}

// TestLowerLimitLocksNobodyOutForGood: failures from before a lower limit end
// with their window, so the right password works again.
func TestLowerLimitLocksNobodyOutForGood(t *testing.T) {
	tr := newThrottle()
	now := time.Now()
	ip := netip.MustParseAddr("192.0.2.80")
	for range 4 {
		tr.fail("mod", ip, now, limits(5, time.Minute), ticket{})
	}
	if _, ok := tr.reserve("mod", ip, now, limits(3, time.Minute)); ok {
		t.Error("failures within the window do not count")
	}
	if _, ok := tr.reserve("mod", ip, now.Add(2*time.Minute), limits(3, time.Minute)); !ok {
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

// TestOwnerLoginGivesNoAttempts covers T-002: the owner signing in between the
// guesses of an attacker neither resets the attacker's count nor the delay.
func TestOwnerLoginGivesNoAttempts(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker, owner := clientAt("198.51.100.7"), clientAt("192.0.2.10")
	guesses := 0
	for range 3 * DefaultMaxFailures {
		if _, _, err := f.svc.Login(ctx, "Mod", testPassword, owner); err != nil {
			t.Fatalf("owner: %v", err)
		}
		if _, _, err := f.svc.Login(ctx, "Mod", "wrong password!!", attacker); errors.Is(err, ErrInvalidCredentials) {
			guesses++
		}
	}
	if guesses != DefaultMaxFailures {
		t.Errorf("%d guesses in one lockout, the limit is %d", guesses, DefaultMaxFailures)
	}
	f.delays = nil
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, owner); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if len(f.delays) != 1 || f.delays[0] != 8*time.Second {
		t.Errorf("delay after the owner's logins = %v, want 8s", f.delays)
	}
}

// TestOwnerLoginDuringABurst covers T-002 with both sides in parallel.
func TestOwnerLoginDuringABurst(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker, owner := clientAt("198.51.100.7"), clientAt("192.0.2.10")
	var wg sync.WaitGroup
	var guessed atomic.Int32
	wg.Go(func() {
		for range 20 {
			if _, _, err := f.svc.Login(ctx, "Mod", testPassword, owner); err != nil {
				t.Errorf("owner: %v", err)
			}
		}
	})
	for range 40 {
		wg.Go(func() {
			if _, _, err := f.svc.Login(ctx, "Mod", "wrong password!!", attacker); errors.Is(err, ErrInvalidCredentials) {
				guessed.Add(1)
			}
		})
	}
	wg.Wait()
	if n := guessed.Load(); n > int32(DefaultMaxFailures) {
		t.Errorf("%d guesses got through, the limit is %d", n, DefaultMaxFailures)
	}
}

// TestLoginEndsOwnFailuresOnly covers Design 3.7: a login takes back the
// failures of its client from the account, those of others stay.
func TestLoginEndsOwnFailuresOnly(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker, owner := clientAt("198.51.100.7"), clientAt("2001:db8:1:2::1")
	wrong := func(c Client, n int) {
		for range n {
			_, _, _ = f.svc.Login(ctx, "Mod", "wrong password!!", c)
		}
	}
	wrong(owner, 2)
	wrong(attacker, 2)
	// Another address of the owner's /64 is the same client.
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("2001:db8:1:2::2")); err != nil {
		t.Fatal(err)
	}
	f.delays = nil
	wrong(owner, 1) // 2 failures left on the account: 1 s
	if !slices.Equal(f.delays, []time.Duration{time.Second}) {
		t.Errorf("delays = %v, want 1s", f.delays)
	}
	// The owner's own count began anew, the attacker's did not.
	wrong(owner, DefaultMaxFailures-2)
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, owner); errors.As(err, &locked) {
		t.Errorf("owner locked after %d failures", DefaultMaxFailures-1)
	}
	wrong(attacker, DefaultMaxFailures-2)
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, attacker); !errors.As(err, &locked) {
		t.Errorf("attacker after %d failures: %v", DefaultMaxFailures, err)
	}
}

// TestLoginAloneEndsTheAccountCount covers the normal case of Design 3.7:
// typing errors of a single client leave no delay behind.
func TestLoginAloneEndsTheAccountCount(t *testing.T) {
	th, ip := newThrottle(), func(i int) netip.Addr { return netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}) }
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for range 3 {
		th.fail("mod", ip(1), now, limits(5, time.Hour), ticket{})
	}
	th.success("mod", ip(1), now, time.Hour)
	if n := len(th.accounts) + len(th.pairs); n != 0 {
		t.Errorf("%d counters left", n)
	}
}

// TestStaleShareStays covers Design 3.7: failures older than the window may
// have left the account counter already, so they take nothing from it.
func TestStaleShareStays(t *testing.T) {
	th, ip := newThrottle(), func(i int) netip.Addr { return netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}) }
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	th.fail("mod", ip(1), now, limits(5, time.Hour), ticket{})
	later := now.Add(2 * time.Hour)
	th.fail("mod", ip(2), later, limits(5, time.Hour), ticket{}) // starts a new account count
	th.success("mod", ip(1), later, time.Hour)
	if d, _ := th.check("mod", ip(3), later, time.Hour); d != 0 {
		t.Fatalf("delay = %v", d)
	}
	if c := th.accounts["mod"]; c == nil || c.failures != 1 {
		t.Errorf("account counter = %+v, want 1 failure", c)
	}
}

// TestR1B1MadeUpNamesGiveNoAttempts is the attack of the review (T-002): four
// guesses, then 5000 made-up names to push the own counters out, then the next
// guesses. Round after round it brings no attempt beyond the limit, and the
// rows it leaves in the audit log stay bounded (T-016).
func TestR1B1MadeUpNamesGiveNoAttempts(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	attacker := clientAt("192.0.2.91")
	checked := 0
	guess := func() {
		if _, _, err := f.svc.Login(ctx, "Mod", "wrong password!!", attacker); errors.Is(err, ErrInvalidCredentials) {
			checked++
		}
	}
	before := f.count(t, `SELECT count(*) FROM audit_log`)
	for round := range 3 {
		for range 4 {
			guess()
		}
		for i := range 5000 {
			_, _, _ = f.svc.Login(ctx, fmt.Sprintf("made-up-%d-%d", round, i), "wrong password!!", attacker)
		}
	}
	if checked > DefaultMaxFailures {
		t.Errorf("%d guesses at the account within one window, the limit is %d", checked, DefaultMaxFailures)
	}
	if got := f.svc.throttle.size(); got > 2*DefaultMaxClientFailures+1 {
		t.Errorf("one client left %d counters", got)
	}
	// A locked client has one row for all names, also those it already tried.
	for i := range DefaultMaxClientFailures {
		_, _, _ = f.svc.Login(ctx, fmt.Sprintf("made-up-0-%d", i), "wrong password!!", attacker)
	}
	// Failed attempts up to the limit, then one row for the lockout of the
	// client and one for its first refused attempt.
	if n := f.count(t, `SELECT count(*) FROM audit_log`) - before; n != DefaultMaxClientFailures+2 {
		t.Errorf("%d audit rows, want %d", n, DefaultMaxClientFailures+2)
	}
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, attacker); !errors.As(err, &locked) {
		t.Errorf("right password from a locked client = %v", err)
	}
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("192.0.2.92")); err != nil {
		t.Errorf("other client is hit by the lockout: %v", err)
	}
}

// TestR1B3AuditRowsPerClientAreBounded covers T-016 with the most rows a client
// can cause: every name up to its own lockout, then the client lockout.
func TestR1B3AuditRowsPerClientAreBounded(t *testing.T) {
	f := newSessionFixture(t, true)
	ctx := context.Background()
	attacker := clientAt("192.0.2.93")
	before := f.count(t, `SELECT count(*) FROM audit_log`)
	for i := range 2000 {
		name := fmt.Sprintf("made-up-%d", i/(DefaultMaxFailures+1))
		_, _, _ = f.svc.Login(ctx, name, "wrong password!!", attacker)
	}
	names := (DefaultMaxClientFailures + DefaultMaxFailures - 1) / DefaultMaxFailures
	limit := DefaultMaxClientFailures + 2*names + 2
	if n := f.count(t, `SELECT count(*) FROM audit_log`) - before; n > limit {
		t.Errorf("%d audit rows from one client, the bound is %d", n, limit)
	}
}

// TestClientLimit covers Design 3.7: failures over all names lock the client
// for the lockout duration. Another address is not touched, and the client is
// free again afterwards.
func TestClientLimit(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	c := clientAt("192.0.2.94")
	for i := range DefaultMaxClientFailures - 1 {
		if _, _, err := f.svc.Login(ctx, fmt.Sprintf("n%d", i), "wrong password!!", c); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v", i, err)
		}
	}
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, c); err != nil {
		t.Fatalf("under the limit: %v", err)
	}
	_, _, _ = f.svc.Login(ctx, "last", "wrong password!!", c)
	logtest.Wait(t, f.logs, `"event":"auth.lockout"`, `"scope":"client"`)
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, c); !errors.As(err, &locked) || locked.RetryAfter != DefaultLockout {
		t.Fatalf("right password of a locked client = %v", err)
	}
	f.now = f.now.Add(DefaultLockout)
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, c); err != nil {
		t.Errorf("after the lockout: %v", err)
	}
}

// TestLoginKeepsTheClientCount: a login to an own account must not reset the
// count of guesses at other accounts.
func TestLoginKeepsTheClientCount(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Own"})
	ctx := context.Background()
	c := clientAt("192.0.2.95")
	guessed := 0
	for i := range 3 * DefaultMaxClientFailures {
		if _, _, err := f.svc.Login(ctx, fmt.Sprintf("n%d", i), "wrong password!!", c); errors.Is(err, ErrInvalidCredentials) {
			guessed++
		}
		_, _, _ = f.svc.Login(ctx, "Own", testPassword, c)
	}
	if guessed != DefaultMaxClientFailures {
		t.Errorf("%d guesses, the limit is %d", guessed, DefaultMaxClientFailures)
	}
}

// TestClientWindowIsFixed: the limit counts per window. A slow trickle, as many
// people behind one address cause it, starts over and never locks.
func TestClientWindowIsFixed(t *testing.T) {
	th := newThrottle()
	now := time.Now()
	ip := netip.MustParseAddr("192.0.2.96")
	lim := loginLimits{maxFailures: 5, maxClientFailures: 3, lockout: time.Hour}
	for i := range 20 {
		if _, client := th.fail(fmt.Sprintf("n%d", i), ip, now.Add(time.Duration(i)*40*time.Minute), lim, ticket{}); client {
			t.Fatalf("failure %d locked a client with two per window", i)
		}
	}
}

// TestClientBurstStaysUnderTheLimit covers T-002 over many names: attempts in
// parallel count before their result, also on the client counter.
func TestClientBurstStaysUnderTheLimit(t *testing.T) {
	f := newSessionFixture(t, true)
	ctx := context.Background()
	c := clientAt("192.0.2.97")
	var wg sync.WaitGroup
	var guessed atomic.Int32
	for i := range 4 * DefaultMaxClientFailures {
		wg.Go(func() {
			if _, _, err := f.svc.Login(ctx, fmt.Sprintf("n%d", i), "wrong password!!", c); errors.Is(err, ErrInvalidCredentials) {
				guessed.Add(1)
			}
		})
	}
	wg.Wait()
	if n := guessed.Load(); n > DefaultMaxClientFailures {
		t.Errorf("%d guesses got through, the limit is %d", n, DefaultMaxClientFailures)
	}
}

// TestIPv6NetworkIsOneClientOverAllNames: rotating inside a /64 starts no new
// client count.
func TestIPv6NetworkIsOneClientOverAllNames(t *testing.T) {
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	ctx := context.Background()
	for i := range DefaultMaxClientFailures {
		_, _, _ = f.svc.Login(ctx, fmt.Sprintf("n%d", i), "wrong password!!", clientAt(fmt.Sprintf("2001:db8:5:6::%x", i+1)))
	}
	var locked *LockedError
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("2001:db8:5:6::ffff")); !errors.As(err, &locked) {
		t.Errorf("another address of the network = %v", err)
	}
	if _, _, err := f.svc.Login(ctx, "Mod", testPassword, clientAt("2001:db8:5:7::1")); err != nil {
		t.Errorf("another network = %v", err)
	}
}

// TestClientLimitSetting covers the setting: a stored value counts, an invalid
// one falls back to the default.
func TestClientLimitSetting(t *testing.T) {
	f := newSessionFixture(t, true)
	ctx := context.Background()
	set := func(v string) {
		t.Helper()
		if _, err := f.st.DB.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ?`, v, SettingMaxClientFailures); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.svc.loginPolicy(ctx).maxClientFailures; got != DefaultMaxClientFailures {
		t.Errorf("default = %d", got)
	}
	set("7")
	if got := f.svc.loginPolicy(ctx).maxClientFailures; got != 7 {
		t.Errorf("stored value = %d", got)
	}
	set("0")
	if got := f.svc.loginPolicy(ctx).maxClientFailures; got != DefaultMaxClientFailures {
		t.Errorf("invalid value = %d", got)
	}
}

// TestV3FailedLoginsAreAuditedUnderLoad covers V-3 on the login: each failure in
// a burst from many addresses leaves its row, also while another writer holds
// the database past the busy timeout.
func TestV3FailedLoginsAreAuditedUnderLoad(t *testing.T) {
	f := newSessionFixture(t, true)
	ctx := context.Background()
	before := f.count(t, `SELECT count(*) FROM audit_log`)
	tx, err := f.st.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET key = key`); err != nil {
		t.Fatal(err)
	}
	var failed atomic.Int32
	var wg sync.WaitGroup
	for i := range 300 {
		wg.Go(func() {
			c := clientAt(fmt.Sprintf("198.51.%d.%d", i/250, i%250))
			if _, _, err := f.svc.Login(ctx, fmt.Sprintf("n%d", i), "wrong password!!", c); errors.Is(err, ErrInvalidCredentials) {
				failed.Add(1)
			}
		})
	}
	time.Sleep(6 * time.Second)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if n, want := f.count(t, `SELECT count(*) FROM audit_log`)-before, int(failed.Load()); n != want || want == 0 {
		t.Errorf("%d audit rows for %d failed logins", n, want)
	}
}

// TestR11DelayGrowsOverAddresses covers #162: failures from many addresses
// make the delay of the account grow to its end, and nobody is locked out.
func TestR11DelayGrowsOverAddresses(t *testing.T) {
	th := newThrottle()
	lim := limits(5, time.Hour)
	now := time.Now()
	want := []time.Duration{0, 0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, maxAccountDelay, maxAccountDelay}
	for i, d := range want {
		ip := netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)})
		delay, locked := th.check("victim", ip, now, lim.lockout)
		if delay != d || locked != 0 {
			t.Errorf("failure %d: delay %v, locked %v", i, delay, locked)
		}
		th.fail("victim", ip, now, lim, ticket{})
	}
}

// TestB5LineStaysBelowARequestTimeout: gap and places together cannot make a
// wait longer than a request lasts, whatever the settings say.
func TestB5LineStaysBelowARequestTimeout(t *testing.T) {
	for _, l := range []loginLimits{
		{overflowGap: 10 * time.Second, overflowLine: 1000},
		{overflowGap: 10 * time.Second},
		{overflowLine: 1000},
		{},
	} {
		if d := time.Duration(l.line()) * l.gap(); d > maxLineWait {
			t.Errorf("%+v: line of %v", l, d)
		}
	}
	if (loginLimits{}).line() != overflowLine {
		t.Error("the default changed")
	}
}
