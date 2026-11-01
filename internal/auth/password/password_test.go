// SPDX-License-Identifier: AGPL-3.0-or-later

package password

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fast keeps unit tests quick; TestDefaultParams covers the real cost.
var fast = Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}

func newHasher(t *testing.T, p Params, n int, wait time.Duration) *Hasher {
	t.Helper()
	h, err := NewHasher(p, n, wait)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHashAndVerify(t *testing.T) {
	ctx := context.Background()
	h := newHasher(t, fast, 2, time.Second)
	enc, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("hash = %q", enc)
	}
	if ok, err := h.Verify(ctx, enc, "correct horse battery"); !ok || err != nil {
		t.Errorf("Verify(correct) = %v, %v", ok, err)
	}
	if ok, err := h.Verify(ctx, enc, "correct horse batterY"); ok || err != nil {
		t.Errorf("Verify(wrong) = %v, %v", ok, err)
	}
	other, _ := h.Hash(ctx, "correct horse battery")
	if other == enc {
		t.Error("two hashes of the same password are equal; salt missing")
	}
	if err := h.VerifyDummy(ctx, "anything"); err != nil {
		t.Errorf("VerifyDummy: %v", err)
	}
}

func TestVerifyUsesStoredParams(t *testing.T) {
	ctx := context.Background()
	old := newHasher(t, Params{Time: 2, MemoryKiB: 128, Threads: 2, SaltLen: 8, KeyLen: 16}, 1, time.Second)
	enc, err := old.Hash(ctx, "a long enough password")
	if err != nil {
		t.Fatal(err)
	}
	// A hasher with other defaults still verifies old hashes.
	if ok, err := newHasher(t, fast, 1, time.Second).Verify(ctx, enc, "a long enough password"); !ok || err != nil {
		t.Errorf("Verify = %v, %v", ok, err)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	h := newHasher(t, fast, 1, time.Second)
	for _, enc := range []string{"", "plain", "$argon2i$v=19$m=64,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=16$m=64,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=64,t=1,p=1$!!$aGFzaA"} {
		if ok, err := h.Verify(context.Background(), enc, "x"); ok || err == nil {
			t.Errorf("Verify(%q) = %v, %v; want error", enc, ok, err)
		}
	}
}

func TestValidateLength(t *testing.T) {
	for pw, want := range map[string]error{
		strings.Repeat("a", 11):  ErrLength,
		strings.Repeat("a", 12):  nil,
		strings.Repeat("ä", 12):  nil, // characters, not bytes
		strings.Repeat("a", 128): nil,
		strings.Repeat("a", 129): ErrLength,
	} {
		if err := Validate(pw); !errors.Is(err, want) {
			t.Errorf("Validate(len %d) = %v, want %v", len(pw), err, want)
		}
	}
}

// TestConcurrencyLimit checks that no more than n hashes run at once and
// that a waiting caller gives up with ErrBusy.
func TestConcurrencyLimit(t *testing.T) {
	h := newHasher(t, fast, 2, 50*time.Millisecond)
	// Occupy both slots.
	r1, err := h.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := h.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := h.Hash(context.Background(), "a long enough password"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Hash with full slots = %v, want ErrBusy", err)
	}
	if d := time.Since(start); d < 40*time.Millisecond {
		t.Errorf("gave up after %v, want the wait time", d)
	}
	r1()
	r2()

	// Many callers at once: never more than two in flight.
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	slow := newHasher(t, fast, 2, 5*time.Second)
	for range 8 {
		wg.Go(func() {
			release, err := slow.acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			inFlight++
			peak = max(peak, inFlight)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			release()
		})
	}
	wg.Wait()
	if peak != 2 {
		t.Errorf("peak parallel hashes = %d, want 2", peak)
	}
}

func TestContextCancelWhileWaiting(t *testing.T) {
	h := newHasher(t, fast, 1, time.Minute)
	release, _ := h.acquire(context.Background())
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "a long enough password"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Hash = %v, want deadline exceeded", err)
	}
}

func TestDefaultParams(t *testing.T) {
	if testing.Short() {
		t.Skip("64 MiB hash")
	}
	h := newHasher(t, Default, 1, time.Second)
	enc, err := h.Hash(context.Background(), "a long enough password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("hash = %q", enc)
	}
}
