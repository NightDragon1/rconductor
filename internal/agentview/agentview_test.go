// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/cache"
)

// slowAgents answers late and at random times of the scheduler, as the
// network does.
type slowAgents struct {
	Agents
	mu     sync.Mutex
	orders []string
}

func (a *slowAgents) Request(_ context.Context, _, typ string, _ any) (agentproto.Result, error) {
	time.Sleep(time.Millisecond)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orders = append(a.orders, typ)
	return agentproto.Result{Code: agentproto.CodeOK}, nil
}

// TestK4StopCannotOvertakeFollow covers #156: after many switches the last
// order wanted is the last one sent.
func TestK4StopCannotOvertakeFollow(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	for round := range 50 {
		a := &slowAgents{}
		v := New(nil, cache.NewAgentData(log), a, nil, log)
		for i := range 11 {
			typ := agentproto.TypeLogsFollow
			if i%2 == 1 {
				typ = agentproto.TypeLogsStop
			}
			v.want(context.Background(), "a1", typ, "mc")
		}
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
			v.mu.Lock()
			idle := len(v.orders) == 0
			v.mu.Unlock()
			if idle {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("orders do not end")
			}
		}
		a.mu.Lock()
		last := a.orders[len(a.orders)-1]
		a.mu.Unlock()
		if last != agentproto.TypeLogsFollow {
			t.Fatalf("round %d: orders end with %s", round, last)
		}
	}
}
