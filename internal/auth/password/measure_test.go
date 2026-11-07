// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build measure

package password

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestMeasureChecksPerHour reports how many password checks an hour allows
// (see the security notes): at most RCD_PASSWORD_HASH_CONCURRENCY run at once,
// for all accounts together. Run with: go test -tags measure -v -run TestMeasureChecksPerHour
func TestMeasureChecksPerHour(t *testing.T) {
	const concurrency, rounds = 2, 40
	h, err := NewHasher(Default, concurrency, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hash, err := h.Hash(ctx, "a password for the measurement")
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	var wg sync.WaitGroup
	for range 4 * concurrency {
		wg.Go(func() {
			for range rounds / 4 {
				if _, err := h.Verify(ctx, hash, "a wrong password"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	took := time.Since(began)
	n := concurrency * rounds
	t.Logf("%d checks at concurrency %d in %v: %.0f ms each in turn, at most %.0f per hour",
		n, concurrency, took.Round(time.Millisecond), float64(took.Milliseconds())/float64(n), float64(n)/took.Hours())
}
