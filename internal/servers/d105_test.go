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
)

// TestD105OneBindingPerContainer: a container is bound to at most one
// server not deleted. A second create or a change onto a bound container is
// ErrContainerBound, a change of the profiles alone never is, a deleted
// server frees its container, and of parallel creates on one container
// exactly one wins.
func TestD105OneBindingPerContainer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.agent("ag")
	profile, host, pw := f.ids["profile"], "mc.lan", logging.Secret(rconPassword)
	create := func(name, agent, container string) (Server, error) {
		in := Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Password: &pw}
		if agent != "" {
			in.AgentID, in.ContainerName = &agent, &container
		}
		return f.svc.Create(ctx, f.as("admin"), in)
	}
	a, err := create("a", "ag", "mc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := create("b", "ag", "mc"); !errors.Is(err, ErrContainerBound) {
		t.Errorf("second create on the container: %v, want ErrContainerBound", err)
	}
	c, err := create("c", "", "")
	if err != nil {
		t.Fatal(err)
	}
	agent, mc, web := "ag", "mc", "web"
	if _, err := f.svc.Update(ctx, f.as("admin"), c.ID, Input{AgentID: &agent, ContainerName: &mc, Version: f.version(c.ID)}); !errors.Is(err, ErrContainerBound) {
		t.Errorf("change onto the bound container: %v, want ErrContainerBound", err)
	}
	if _, err := f.svc.Update(ctx, f.as("admin"), c.ID, Input{AgentID: &agent, ContainerName: &web, Version: f.version(c.ID)}); err != nil {
		t.Errorf("change onto a free container: %v", err)
	}
	// The profiles alone of a bound server: no binding is written anew.
	if _, err := f.svc.Update(ctx, f.as("admin"), a.ID, Input{ProfileIDs: &[]string{profile}, Version: f.version(a.ID)}); err != nil {
		t.Errorf("profiles of a bound server: %v", err)
	}
	if err := f.svc.Delete(ctx, f.as("admin"), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := create("d", "ag", "mc"); err != nil {
		t.Errorf("the container of a deleted server: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, bound := 0, 0
	for i := range 8 {
		wg.Go(func() {
			_, err := create(fmt.Sprintf("p%d", i), "ag", "race")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrContainerBound):
				bound++
			default:
				t.Errorf("parallel create %d: %v", i, err)
			}
		})
	}
	wg.Wait()
	if won != 1 || bound != 7 {
		t.Errorf("parallel creates: %d won, %d refused, want 1 and 7", won, bound)
	}
}
