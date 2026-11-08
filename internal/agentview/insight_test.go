// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// fakeAgents answers version.detect with the next prepared version and
// volumes.size with fixed sizes; it records the orders.
type fakeAgents struct {
	mu       sync.Mutex
	caps     []string
	versions []string
	detects  []agentproto.VersionDetect
	volumes  int
}

func (a *fakeAgents) Request(context.Context, string, string, any) (agentproto.Result, error) {
	return agentproto.Result{Code: agentproto.CodeOK}, nil
}

func (a *fakeAgents) RequestData(_ context.Context, _, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch typ {
	case agentproto.TypeVersionDetect:
		d := payload.(agentproto.VersionDetect)
		a.detects = append(a.detects, d)
		v := ""
		if len(a.versions) > 0 {
			v, a.versions = a.versions[0], a.versions[1:]
		}
		e, _ := agentproto.New(agentproto.TypeVersionFound, "", agentproto.VersionFound{Container: d.Container, Version: v})
		collect(e)
	case agentproto.TypeVolumesSize:
		a.volumes++
		e, _ := agentproto.New(agentproto.TypeVolumeSizes, "", agentproto.VolumeSizes{Volumes: []agentproto.VolumeSize{
			{Name: "mc-data", Bytes: 3 << 30}, {Name: "mc-logs", Bytes: -1},
		}})
		collect(e)
	}
	return agentproto.Result{Code: agentproto.CodeOK}, nil
}

func (a *fakeAgents) Capabilities(string) ([]string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.caps), true
}

func (a *fakeAgents) orders() ([]agentproto.VersionDetect, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.detects), a.volumes
}

type nullBus struct {
	mu       sync.Mutex
	runtimes int
}

func (*nullBus) PublishAgentData(string, string, any) {}
func (b *nullBus) PublishRuntime(string, any, any) {
	b.mu.Lock()
	b.runtimes++
	b.mu.Unlock()
}

type insightFixture struct {
	st    *store.Store
	data  *cache.AgentData
	ag    *fakeAgents
	view  *View
	clock time.Time
}

// newInsightFixture has the servers s1 and s2 on the container mc of
// agent a1 with a version pattern in their profile.
func newInsightFixture(t *testing.T, caps ...string) *insightFixture {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, imported_at)
		VALUES ('p1', 'mc', 'mc', 'source-rcon', 25575, '{"version":{"pattern":"version (\\S+)"}}', 'f', 'h', 't')`)
	exec(`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('a1', 'host', x'00', 'fp', 'active', 't')`)
	for _, id := range []string{"s1", "s2"} {
		exec(`INSERT INTO servers (id, name, created_at, updated_at, profile_id, agent_id, container_name) VALUES (?, ?, 't', 't', 'p1', 'a1', 'mc')`, id, id)
	}
	log := slog.New(slog.DiscardHandler)
	f := &insightFixture{st: st, data: cache.NewAgentData(log), ag: &fakeAgents{caps: caps}}
	f.view = New(st, f.data, f.ag, &nullBus{}, log)
	f.clock = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	f.view.in.now = func() time.Time { return f.clock }
	return f
}

func (f *insightFixture) start(at time.Time) {
	f.data.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{
		{ID: "c-" + at.Format("150405"), Name: "mc", Image: "img", State: "running", StartedAt: at, Volumes: []string{"mc-data", "mc-logs"}},
	}})
}

// TestVersionSearch covers AGT-006 in the panel: the first attempt reads
// from the container start, a later one only since the last attempt and
// only after agent.version_retry; a match ends the search and is a server
// datum for every role; a new start forgets it and searches again; after
// agent.version_window the search stops.
func TestVersionSearch(t *testing.T) {
	f := newInsightFixture(t, agentproto.CapVersionDetect)
	f.ag.versions = []string{"", "", "1.21.4"} // s1, s2, then s1
	ctx := context.Background()
	t0 := f.clock
	f.start(t0)

	f.clock = t0.Add(time.Second)
	f.view.Pass(ctx)
	f.clock = t0.Add(10 * time.Second)
	f.view.Pass(ctx) // not due yet
	d, _ := f.ag.orders()
	if len(d) != 2 || !d[0].Since.Equal(t0) || d[0].Pattern != `version (\S+)` || d[0].Container != "mc" {
		t.Fatalf("first attempts = %+v, want one per server from the start", d)
	}
	if v := f.view.Runtime(ctx, "s1", false).GameVersion; v != "" {
		t.Errorf("version before a match = %q", v)
	}

	f.clock = t0.Add(31 * time.Second)
	f.view.Pass(ctx)
	d, _ = f.ag.orders()
	if len(d) != 4 || !d[2].Since.Equal(t0.Add(time.Second)) {
		t.Fatalf("second attempt = %+v, want since the first attempt", d)
	}
	// s1 got the match, s2 the empty answer of the queue's end.
	for _, agentData := range []bool{true, false} {
		if v := f.view.Runtime(ctx, "s1", agentData).GameVersion; v != "1.21.4" {
			t.Errorf("version (agent data %v) = %q", agentData, v)
		}
	}
	f.clock = t0.Add(5 * time.Minute)
	f.view.Pass(ctx)
	if d, _ = f.ag.orders(); len(d) != 5 {
		t.Errorf("orders after the match = %d, want only s2 again", len(d))
	}

	// A new start forgets the version and reads from the new start.
	t1 := t0.Add(20 * time.Minute)
	f.clock = t1
	f.ag.versions = nil
	f.start(t1)
	f.view.Pass(ctx)
	if v := f.view.Runtime(ctx, "s1", false).GameVersion; v != "" {
		t.Errorf("version after a restart = %q", v)
	}
	d, _ = f.ag.orders()
	if last := d[len(d)-1]; !last.Since.Equal(t1) {
		t.Errorf("after a restart since = %v, want %v", last.Since, t1)
	}
	// After the window, counted from when the panel saw this run, no
	// attempt.
	n := len(d)
	f.clock = t1.Add(DefaultVersionWindow + time.Second)
	f.view.Pass(ctx)
	if d, _ = f.ag.orders(); len(d) != n {
		t.Errorf("attempts after the window: %d, want %d", len(d), n)
	}
}

// TestVersionRetrySetting covers the settings of the search: the retry
// follows agent.version_retry and never goes below its minimum.
func TestVersionRetrySetting(t *testing.T) {
	f := newInsightFixture(t, agentproto.CapVersionDetect)
	ctx := context.Background()
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE settings SET value = '"1s"' WHERE key = ?`, SettingVersionRetry); err != nil {
		t.Fatal(err)
	}
	t0 := f.clock
	f.start(t0)
	for _, after := range []time.Duration{0, 2 * time.Second, 4 * time.Second, MinVersionRetry} {
		f.clock = t0.Add(after)
		f.view.Pass(ctx)
	}
	if d, _ := f.ag.orders(); len(d) != 4 {
		t.Errorf("attempts = %d, want 2 per server: at 0 and at the minimum of %v", len(d), MinVersionRetry)
	}
}

// TestOlderAgentNoVersionNoSize covers the acceptance of AGT-006 and
// AGT-007 for an agent 1.0: without the capabilities the panel asks
// nothing, shows no version and no size, and the volume names stay.
func TestOlderAgentNoVersionNoSize(t *testing.T) {
	f := newInsightFixture(t, agentproto.CapLogs, agentproto.CapMetrics)
	ctx := context.Background()
	f.start(f.clock)
	f.view.Pass(ctx)
	if d, vols := f.ag.orders(); len(d) != 0 || vols != 0 {
		t.Errorf("orders to an older agent: %d detects, %d sizes", len(d), vols)
	}
	rt := f.view.Runtime(ctx, "s1", true)
	if rt.GameVersion != "" || rt.Container == nil || len(rt.Container.Volumes) != 2 {
		t.Fatalf("runtime = %+v", rt)
	}
	for _, vv := range rt.Container.Volumes {
		if vv.SizeBytes != nil || vv.MeasuredAt != nil {
			t.Errorf("size without the capability: %+v", vv)
		}
	}
}

// TestVolumeSizes covers AGT-007 in the panel: one order per agent and
// interval for all its servers; the size carries its time, unknown sizes
// stay empty, and after agent.volume_stale the value is marked old;
// moderators get no container part.
func TestVolumeSizes(t *testing.T) {
	f := newInsightFixture(t, agentproto.CapVolumesSize)
	ctx := context.Background()
	t0 := f.clock
	f.start(t0)
	f.view.Pass(ctx)
	f.clock = t0.Add(time.Hour)
	f.view.Pass(ctx)
	if _, vols := f.ag.orders(); vols != 1 {
		t.Fatalf("volume orders = %d, want one for two servers within the interval", vols)
	}
	rt := f.view.Runtime(ctx, "s2", true)
	data, logs := rt.Container.Volumes[0], rt.Container.Volumes[1]
	if data.Name != "mc-data" || data.SizeBytes == nil || *data.SizeBytes != 3<<30 || !data.MeasuredAt.Equal(t0) || data.Stale {
		t.Errorf("mc-data = %+v", data)
	}
	if logs.Name != "mc-logs" || logs.SizeBytes != nil {
		t.Errorf("unknown size = %+v", logs)
	}
	f.clock = t0.Add(DefaultVolumeStale + time.Minute)
	if rt = f.view.Runtime(ctx, "s2", true); !rt.Container.Volumes[0].Stale {
		t.Error("size older than agent.volume_stale not marked")
	}
	f.view.Pass(ctx)
	if _, vols := f.ag.orders(); vols != 2 {
		t.Errorf("volume orders after the interval = %d, want 2", vols)
	}
	if rt := f.view.Runtime(ctx, "s2", false); rt.Container != nil {
		t.Error("container part for a role without agent data")
	}
}

// TestInsightConcurrent runs passes, restarts and reads of the runtime at
// once; the race detector checks the shared state.
func TestInsightConcurrent(t *testing.T) {
	f := newInsightFixture(t, agentproto.CapVersionDetect, agentproto.CapVolumesSize)
	ctx := context.Background()
	t0 := f.clock
	f.view.in.now = func() time.Time { return t0 }
	f.start(t0)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 20 {
				switch i % 3 {
				case 0:
					f.view.Pass(ctx)
				case 1:
					f.start(t0.Add(time.Duration(j) * time.Second))
				default:
					_ = f.view.Runtime(ctx, "s1", true)
				}
			}
		})
	}
	wg.Wait()
}

// TestVersionOfALongRunningContainer covers a panel that starts while the
// container runs for days: the window counts from the panel's first sight,
// the search still reads from the container start (AGT-006).
func TestVersionOfALongRunningContainer(t *testing.T) {
	f := newInsightFixture(t, agentproto.CapVersionDetect)
	f.ag.versions = []string{"26.3", "26.3"}
	ctx := context.Background()
	started := f.clock.Add(-50 * time.Hour)
	f.start(started)
	f.view.Pass(ctx)
	d, _ := f.ag.orders()
	if len(d) != 2 || !d[0].Since.Equal(started) {
		t.Fatalf("attempts = %+v, want from the container start", d)
	}
	if v := f.view.Runtime(ctx, "s1", false).GameVersion; v != "26.3" {
		t.Errorf("version = %q", v)
	}
}
