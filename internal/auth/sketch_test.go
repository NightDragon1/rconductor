// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"fmt"
	"hash/maphash"
	"net/netip"
	"testing"
	"time"
)

// neighbor is a name that shares only the slot of row 1 with name.
func neighbor(k *sketch, name string) string {
	want := maphash.String(k.rows[1].seed, name) % sketchSlots
	notIn0 := maphash.String(k.rows[0].seed, name) % sketchSlots
	for i := 0; ; i++ {
		n := fmt.Sprintf("x%d", i)
		if maphash.String(k.rows[1].seed, n)%sketchSlots == want && maphash.String(k.rows[0].seed, n)%sketchSlots != notIn0 {
			return n
		}
	}
}

// TestR5B21SketchWindowHolds: a look at another name in one of the two slots
// staggered their windows, and the count fell to 0 when the first one ended.
func TestR5B21SketchWindowHolds(t *testing.T) {
	const window = time.Hour
	t0 := time.Now()
	th := fullTable(t0)
	lim := limits(5, window)
	attacker := netip.MustParseAddr("203.0.113.9")
	name := "victim"
	th.overflow.count(neighbor(th.overflow, pairName(pairKey{name, attacker})), t0.Add(-window/2), window)
	th.overflow.count(neighbor(th.overflow, clientName(attacker)), t0.Add(-window/2), window)
	guesses := 0
	for now := t0; now.Before(t0.Add(window)); now = now.Add(10 * time.Second) {
		if attempt(t, th, name, attacker, now, lim) {
			guesses++
		}
	}
	if guesses > 5 {
		t.Errorf("%d guesses from one client within one window, the limit is 5", guesses)
	}
}

// TestR5B21SketchUnderTraffic is the same without aiming: the names of a flood
// touch the slots.
func TestR5B21SketchUnderTraffic(t *testing.T) {
	const window = 15 * time.Minute
	t0 := time.Now()
	th := fullTable(t0)
	lim := limits(5, window)
	n := 0
	for now := t0.Add(-5 * time.Minute); now.Before(t0); now = now.Add(2 * time.Millisecond) {
		n++
		ip := netip.AddrFrom4([4]byte{10, byte(n >> 16), byte(n >> 8), byte(n)})
		th.overflow.count(pairName(pairKey{fmt.Sprintf("r%d", n), ip}), now, window)
		th.overflow.count(clientName(ip), now, window)
	}
	for r := range 50 {
		attacker := netip.AddrFrom4([4]byte{198, 18, byte(r >> 8), byte(r)})
		guesses := 0
		for now := t0; now.Before(t0.Add(window)); now = now.Add(5 * time.Second) {
			if attempt(t, th, "victim", attacker, now, lim) {
				guesses++
			}
		}
		if guesses > 5 {
			t.Fatalf("%d guesses from one client within one window, the limit is 5", guesses)
		}
	}
}

// TestR5B23EntriesPerAddress: the fixed window of the client counter lets one
// address fail 49 times at its end and 50 times after it. R-12 counts with that.
func TestR5B23EntriesPerAddress(t *testing.T) {
	const window = 15 * time.Minute
	t0 := time.Now()
	th := newThrottle()
	lim := limits(5, window)
	ip := netip.MustParseAddr("203.0.113.9")
	try := func(n int, at time.Time) bool {
		name := fmt.Sprintf("n%d", n)
		tk, ok := th.reserve(name, ip, at, lim)
		if !ok {
			return false
		}
		th.fail(name, ip, at, lim, tk)
		th.release(name, ip, tk)
		return true
	}
	done := 1
	try(0, t0)
	for done < 49 && try(done, t0.Add(window-time.Second)) {
		done++
	}
	for try(done, t0.Add(window)) {
		done++
	}
	th.crowded(1<<30, t0.Add(window), window)
	if n := th.size(); n > 197 {
		t.Errorf("%d entries of one address, R-12 counts with 197", n)
	}
}
