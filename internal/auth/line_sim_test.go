// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"sort"
	"testing"
	"time"
)

// The simulation of the overflow line comes from the blind review (round 4,
// B-2). It runs the real reserve, fail and nextTurn in virtual time and rebuilds
// only the end of a turn.

type lineResult struct {
	logins        int
	median        time.Duration
	longest       time.Duration
	attackerTurns int
}

type pend struct {
	name  string
	ip    netip.Addr
	tk    ticket
	owner bool
}

// simulateLine runs the line in virtual time. withClientCounter: the attackers hold a
// client counter in the table, as those that filled it do.
func simulateLine(t *testing.T, n int, every time.Duration, withClientCounter bool, duration time.Duration) lineResult {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	th := newThrottle()
	th.capacity = 10_000_000
	lim := loginLimits{maxFailures: 5, maxClientFailures: 50, lockout: 15 * time.Minute, maxTracked: 10_000_000}
	addr := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}) }
	filler := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{11, byte(i >> 16), byte(i >> 8), byte(i)}) }
	for i := range n {
		ip := filler(i)
		if withClientCounter {
			ip = addr(i)
		}
		th.fail(fmt.Sprintf("fill%d", i), ip, base, lim, ticket{})
	}
	lim.maxTracked = th.size()
	th.capacity = th.size()

	const tick = time.Millisecond
	next := make([]time.Duration, n)
	for i := range next {
		next[i] = time.Duration(i) * every / time.Duration(n)
	}
	seq := 0
	var pending []*pend
	var holder *pend
	var turnEnd time.Duration
	ownerNext := 60 * time.Second
	ownerStart := time.Duration(-1)
	ownerName := "owner"
	ownerIP := netip.MustParseAddr("192.0.2.1")
	var waits []time.Duration
	attackerRuns, attackerRefused, ownerTries, ownerRefused := 0, 0, 0, 0
	pushed := 0

	for now := time.Duration(0); now < duration; now += tick {
		at := base.Add(now)
		ownerFirst := rand.IntN(2) == 0 && ownerNext <= now
		if ownerFirst {
			ownerStep := func() {}
			_ = ownerStep
		}
		for _, i := range rand.Perm(len(next)) {
			for next[i] <= now {
				next[i] += time.Duration(float64(every) * (0.5 + rand.Float64()))
				seq++
				name := fmt.Sprintf("a%d", seq)
				tk, ok := th.reserve(name, addr(i), at, lim)
				if !ok {
					attackerRefused++
					continue
				}
				if tk.place == nil {
					th.fail(name, addr(i), at, lim, tk)
					th.release(name, addr(i), tk)
					continue
				}
				pending = append(pending, &pend{name: name, ip: addr(i), tk: tk})
			}
		}
		if ownerNext <= now {
			ownerNext = now + time.Duration(float64(time.Second)*(0.5+rand.Float64()))
			if ownerStart < 0 {
				ownerStart = now
			}
			ownerTries++
			tk, ok := th.reserve(ownerName, ownerIP, at, lim)
			switch {
			case !ok:
				ownerRefused++
			case tk.place == nil:
				t.Fatal("owner without line")
			default:
				pending = append(pending, &pend{name: ownerName, ip: ownerIP, tk: tk, owner: true})
			}
		}
		keep := pending[:0]
		for _, p := range pending {
			select {
			case turn := <-p.tk.place.answer:
				if turn {
					holder = p
					turnEnd = now + overflowGap
				} else {
					if p.owner {
						pushed++
					}
					th.release(p.name, p.ip, p.tk)
				}
			default:
				keep = append(keep, p)
			}
		}
		pending = keep
		if holder != nil && now >= turnEnd {
			h := holder
			holder = nil
			th.mu.Lock()
			th.turnTaken = false
			th.nextTurn()
			th.mu.Unlock()
			if h.owner {
				th.success(h.name, h.ip, at, lim.lockout)
				th.release(h.name, h.ip, h.tk)
				waits = append(waits, now-ownerStart)
				ownerStart = -1
				ownerNext = now + 10*time.Second
			} else {
				attackerRuns++
				th.fail(h.name, h.ip, at, lim, h.tk)
				th.release(h.name, h.ip, h.tk)
			}
		}
	}
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	med, maxw := time.Duration(0), time.Duration(0)
	if len(waits) > 0 {
		med, maxw = waits[len(waits)/2], waits[len(waits)-1]
	}
	still := time.Duration(0)
	if ownerStart >= 0 {
		still = duration - ownerStart
	}
	return lineResult{logins: len(waits), median: med, longest: max(maxw, still), attackerTurns: attackerRuns}
}

// TestR4B2OwnerGetsThroughCountedAttackers: the addresses that filled the table
// have client counters there. Before the fix they kept rank 0 in the line and
// the owner waited over 11 minutes at 100 addresses, or forever.
func TestR4B2OwnerGetsThroughCountedAttackers(t *testing.T) {
	r := simulateLine(t, 100, time.Second, true, 4*time.Minute)
	t.Logf("%+v", r)
	if r.logins == 0 || r.longest > 5*time.Second {
		t.Errorf("owner: %d logins, longest wait %v", r.logins, r.longest)
	}
	if r.attackerTurns > 100*DefaultMaxFailures {
		t.Errorf("%d attacker turns, the limit is %d", r.attackerTurns, 100*DefaultMaxFailures)
	}
}
