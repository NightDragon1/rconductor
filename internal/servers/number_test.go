// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestServerNumber covers UI-018 and D-087: servers get consecutive
// numbers; a number freed by a delete, soft or hard, is never given again;
// List and Get carry it.
func TestServerNumber(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.create("a"), f.create("b")
	if a.Number != 1 || b.Number != 2 {
		t.Fatalf("numbers %d, %d, want 1, 2", a.Number, b.Number)
	}
	if err := f.svc.Delete(ctx, f.as("admin"), b.ID); err != nil {
		t.Fatal(err)
	}
	if c := f.create("c"); c.Number != 3 {
		t.Errorf("after a delete: number %d, want 3", c.Number)
	}
	// The row of the highest number goes for good (D-028).
	f.exec(`DELETE FROM servers WHERE name = 'c'`)
	if d := f.create("d"); d.Number != 4 {
		t.Errorf("after the row went: number %d, want 4", d.Number)
	}
	// A counter set back is an error, not a name conflict.
	f.exec(`UPDATE counters SET value = 0 WHERE name = 'server_number'`)
	name, host, pw, profile := "e", "mc.lan", logging.Secret(rconPassword), f.ids["profile"]
	_, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Password: &pw})
	if inv := (*users.InvalidError)(nil); err == nil || errors.As(err, &inv) {
		t.Errorf("number taken twice: %v, want an internal error", err)
	}
	f.exec(`UPDATE counters SET value = 4 WHERE name = 'server_number'`)
	list, err := f.svc.List(ctx, f.as("admin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Number == 0 {
			t.Errorf("server %s listed without number", s.Name)
		}
	}
	if got, err := f.svc.Get(ctx, f.as("admin"), a.ID); err != nil || got.Number != 1 {
		t.Errorf("get = %d %v", got.Number, err)
	}
}

// TestServerNumberParallel covers UI-018: 50 servers created at once get
// 50 distinct numbers.
func TestServerNumberParallel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const n = 50
	var mu sync.Mutex
	seen := map[int64]string{}
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			name, host, pw, profile := fmt.Sprintf("s%02d", i), "mc.lan", logging.Secret(rconPassword), f.ids["profile"]
			s, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Password: &pw})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("create %s: %v", name, err)
				return
			}
			if other, ok := seen[s.Number]; ok {
				t.Errorf("number %d given to %s and %s", s.Number, other, name)
			}
			seen[s.Number] = name
		})
	}
	wg.Wait()
	for i := int64(1); i <= n; i++ {
		if _, ok := seen[i]; !ok {
			t.Errorf("number %d missing", i)
		}
	}
}
