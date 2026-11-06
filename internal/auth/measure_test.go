// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build measure

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These measurements back the residual risks of Sicherheit chapter 5. They
// take minutes and run with make measure-login.

// TestMeasureCounterTableMemory is the memory of a full table (R-12).
func TestMeasureCounterTableMemory(t *testing.T) {
	for _, nameLen := range []int{12, maxAccountKey} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		th := newThrottle()
		now := time.Now()
		pad := strings.Repeat("x", nameLen-8)
		for i := 0; th.size() < DefaultMaxTracked-2; i++ {
			ip := netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
			th.fail(fmt.Sprintf("%s%08d", pad, i), ip, now, limits(5, time.Hour), ticket{})
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("names of %d bytes: %d entries, %d MiB with the sketch", nameLen, th.size(), (after.HeapAlloc-before.HeapAlloc)>>20)
		runtime.KeepAlive(th)
	}
}

// TestMeasureLineUnderCountedAttackers runs the simulation of the blind review
// (B-2) at full size, up to 1000 addresses with five requests a second each.
func TestMeasureLineUnderCountedAttackers(t *testing.T) {
	for _, n := range []int{100, 300, 1000} {
		for _, every := range []time.Duration{time.Second, time.Second / 5} {
			r := simulateLine(t, n, every, true, 14*time.Minute)
			t.Logf("%d addresses, one request every %v: %d logins, median %v, longest %v, attacker turns %d", n, every, r.logins, r.median, r.longest, r.attackerTurns)
			if r.logins == 0 || r.longest > 5*time.Second {
				t.Errorf("%d addresses: owner got %d logins, longest wait %v", n, r.logins, r.longest)
			}
		}
	}
}

// TestMeasureOwnerWaitOnAFullTable is the wait of the owner of an account, in
// real time, while n addresses without counters try one made-up name a second
// each on a full table (R-12). Three sign-ins, the first soon after the start,
// when the attackers still have no failures.
func TestMeasureOwnerWaitOnAFullTable(t *testing.T) {
	const patience = 5 * time.Minute
	for _, n := range []int{100, 300, 1000} {
		f := fullFixture(t)
		f.svc.throttle = fullTable(time.Now())
		f.svc.now = time.Now
		f.svc.sleep = func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		start := time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		var guesses atomic.Int64
		for i := range n {
			wg.Go(func() {
				c := clientAt(fmt.Sprintf("10.20.%d.%d", i/250, i%250))
				for k := 0; ctx.Err() == nil; k++ {
					if _, _, err := f.svc.Login(ctx, fmt.Sprintf("x%d-%d", i, k), "wrong password!!", c); errors.Is(err, ErrInvalidCredentials) {
						guesses.Add(1)
					}
					time.Sleep(time.Second)
				}
			})
		}
		var waits []string
		for round := range 3 {
			time.Sleep(time.Duration(2+round*30) * time.Second)
			began := time.Now()
			for {
				_, _, err := f.svc.Login(context.Background(), "Mod", testPassword, clientAt(fmt.Sprintf("10.8.0.%d", round+1)))
				if err == nil {
					break
				}
				if time.Since(began) > patience {
					t.Errorf("%d addresses: owner still out after %v", n, patience)
					break
				}
				time.Sleep(5 * time.Second)
			}
			waits = append(waits, time.Since(began).Round(time.Second).String())
		}
		cancel()
		wg.Wait()
		t.Logf("%d addresses: owner waited %v; %d guesses in %v", n, waits, guesses.Load(), time.Since(start).Round(time.Second))
		if int(guesses.Load()) > n*DefaultMaxFailures {
			t.Errorf("%d guesses from %d addresses, the limit is %d each", guesses.Load(), n, DefaultMaxFailures)
		}
	}
}

// TestMeasureR5B22HangUpHoldsTheLine: on a full table, attackers from a few addresses
// hang up before their turn ends (under overflowGap after the request). They
// never reach the password check, so they keep 0 failures, and an owner with 0
// failures cannot push them out. Real time, real Login.
func TestMeasureR5B22HangUpHoldsTheLine(t *testing.T) {
	addrs, _ := strconv.Atoi(os.Getenv("POC_ADDRS"))
	if addrs == 0 {
		addrs = 60
	}
	secs, _ := strconv.Atoi(os.Getenv("POC_SECS"))
	if secs == 0 {
		secs = 60
	}
	f := fullFixture(t)
	f.svc.sleep = sleepCtx
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var sent, refused, hungUp, reached atomic.Int64
	for i := range addrs {
		c := clientAt(fmt.Sprintf("203.0.113.%d", i+1))
		wg.Go(func() {
			life, _ := time.ParseDuration(os.Getenv("POC_LIFE"))
			if life == 0 {
				life = 200 * time.Millisecond
			}
			time.Sleep(time.Duration(i) * life / time.Duration(addrs))
			for n := 0; ctx.Err() == nil; n++ {
				rctx, rcancel := context.WithTimeout(ctx, life)
				_, _, err := f.svc.Login(rctx, fmt.Sprintf("guess%d-%d", i, n), "wrong password!!", c)
				rcancel()
				sent.Add(1)
				var locked *LockedError
				switch {
				case errors.As(err, &locked):
					refused.Add(1)
					time.Sleep(2 * time.Millisecond)
				case errors.Is(err, ErrInvalidCredentials):
					reached.Add(1)
				default:
					hungUp.Add(1)
				}
			}
		})
	}
	time.Sleep(time.Second)
	tries, in := 0, 0
	start := time.Now()
	var firstIn time.Duration
	for ctx.Err() == nil {
		tries++
		octx, ocancel := context.WithTimeout(ctx, 10*time.Second)
		_, _, err := f.svc.Login(octx, "Mod", testPassword, clientAt("192.0.2.77"))
		ocancel()
		if err == nil {
			in++
			if firstIn == 0 {
				firstIn = time.Since(start)
			}
			t.Logf("owner in after %d tries, %v", tries, time.Since(start).Round(time.Millisecond))
			tries, start = 0, time.Now()
			// a login adds no counter of the owner's client, so the next round is a fresh owner
			continue
		}
		time.Sleep(time.Second) // Retry-After: 1
	}
	if in == 0 {
		cancel()
	}
	wg.Wait()
	failed := 0
	for i := range addrs {
		ip := netip.MustParseAddr(fmt.Sprintf("203.0.113.%d", i+1))
		_ = ip
	}
	t.Logf("addresses %d: requests %d, refused %d, hung up %d, reached password check %d; failures counted for them: %d",
		addrs, sent.Load(), refused.Load(), hungUp.Load(), reached.Load(), failed)
	t.Logf("owner: %d tries in %v, signed in: %d (first at %v)", tries, time.Since(start).Round(time.Second), in, firstIn)
}

// neighbour finds a name that shares the slot of name in row 1 but not in row 0.
