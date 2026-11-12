// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/cache"
)

// daemonEvents serves events like the daemon: an event reaches only the
// subscriptions that exist when it happens. The first snapshot list sees
// mc stopped; right after the list mc starts.
type daemonEvents struct {
	*fakeDocker
	mu     sync.Mutex
	subs   []chan events.Message
	listed bool
}

func (f *daemonEvents) Events(ctx context.Context, _ client.EventsListOptions) client.EventsResult {
	ch := make(chan events.Message, 8)
	f.mu.Lock()
	f.subs = append(f.subs, ch)
	f.mu.Unlock()
	errs := make(chan error)
	go func() { <-ctx.Done(); close(errs) }()
	return client.EventsResult{Messages: ch, Err: errs}
}

func (f *daemonEvents) ContainerList(ctx context.Context, o client.ContainerListOptions) (client.ContainerListResult, error) {
	r, err := f.fakeDocker.ContainerList(ctx, o)
	f.mu.Lock()
	first := !f.listed && len(o.Filters["id"]) == 0 && len(o.Filters["status"]) == 0
	f.listed = true
	f.mu.Unlock()
	if first {
		// The list is what it was; then the container starts.
		f.fakeDocker.mu.Lock()
		f.fakeDocker.items[0].State = "running"
		f.fakeDocker.mu.Unlock()
		f.publish(events.Message{Type: "container", Action: "start", Actor: events.Actor{ID: "id-mc", Attributes: map[string]string{"name": "mc"}}})
	}
	return r, err
}

func (f *daemonEvents) publish(m events.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.subs {
		ch <- m
	}
}

// panelSender hands what the backend sends to the panel's agent data.
type panelSender struct{ data *cache.AgentData }

func (p panelSender) Send(_ context.Context, typ, ref string, payload any) error {
	e, err := agentproto.New(typ, ref, payload)
	if err != nil {
		return err
	}
	p.data.Receive("a1", e)
	return nil
}

// TestO2StateChangeWhileTheAgentStarts covers AGT-003 and NFR-005: a
// container that changes its state while the backend takes its snapshot
// shows its current state in the panel, without any later event.
func TestO2StateChangeWhileTheAgentStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, fd, _ := newTestDocker(t)
		fd.items[0].State = "exited"
		f := &daemonEvents{fakeDocker: fd}
		d.api = f
		data := cache.NewAgentData(slog.New(slog.NewTextHandler(io.Discard, nil)))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- d.Run(ctx, panelSender{data}) }()
		synctest.Wait()
		c, ok := data.Container("a1", "mc")
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !ok || c.State != "running" {
			t.Errorf("panel shows %q (found %v), container runs", c.State, ok)
		}
	})
}
