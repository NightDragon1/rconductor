// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
)

// fakeDocker answers like the daemon for a fixed set of containers and
// records every call that changes something.
type fakeDocker struct {
	mu      sync.Mutex
	items   []container.Summary
	actions []string
	logs    []byte
	tty     bool
	events  chan events.Message
}

func (f *fakeDocker) ContainerList(_ context.Context, o client.ContainerListOptions) (client.ContainerListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []container.Summary
	for _, s := range f.items {
		ok := true
		for name := range o.Filters["name"] {
			ok = ok && "^"+s.Names[0]+"$" == name
		}
		for id := range o.Filters["id"] {
			ok = ok && s.ID == id
		}
		for st := range o.Filters["status"] {
			ok = ok && string(s.State) == st
		}
		// The fake ignores the label filter, so the agent's own check
		// is what keeps unlabeled containers out.
		if ok {
			out = append(out, s)
		}
	}
	return client.ContainerListResult{Items: out}, nil
}

func (f *fakeDocker) ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: container.InspectResponse{
		Config: &container.Config{Tty: f.tty}, RestartCount: 2,
		State: &container.State{StartedAt: "2026-11-03T10:00:00.5Z"},
	}}, nil
}

func (f *fakeDocker) record(a string) {
	f.mu.Lock()
	f.actions = append(f.actions, a)
	f.mu.Unlock()
}

func (f *fakeDocker) ContainerStart(_ context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	f.record("start " + id)
	return client.ContainerStartResult{}, nil
}

func (f *fakeDocker) ContainerStop(_ context.Context, id string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
	f.record("stop " + id)
	return client.ContainerStopResult{}, nil
}

func (f *fakeDocker) ContainerRestart(_ context.Context, id string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	f.record("restart " + id)
	return client.ContainerRestartResult{}, nil
}

func (f *fakeDocker) ContainerStats(context.Context, string, client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	b, _ := json.Marshal(sample())
	return client.ContainerStatsResult{Body: io.NopCloser(bytes.NewReader(b))}, nil
}

func (f *fakeDocker) ContainerLogs(context.Context, string, client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	return io.NopCloser(bytes.NewReader(f.logs)), nil
}

func (f *fakeDocker) Events(ctx context.Context, _ client.EventsListOptions) client.EventsResult {
	errs := make(chan error)
	go func() { <-ctx.Done(); close(errs) }()
	return client.EventsResult{Messages: f.events, Err: errs}
}

func sample() container.StatsResponse {
	var st container.StatsResponse
	st.CPUStats.CPUUsage.TotalUsage, st.PreCPUStats.CPUUsage.TotalUsage = 3_000, 1_000
	st.CPUStats.SystemUsage, st.PreCPUStats.SystemUsage = 20_000, 10_000
	st.CPUStats.OnlineCPUs = 4
	st.MemoryStats.Usage, st.MemoryStats.Limit = 500, 2_000
	st.MemoryStats.Stats = map[string]uint64{"inactive_file": 100}
	st.Networks = map[string]container.NetworkStats{"eth0": {RxBytes: 10, TxBytes: 20}, "eth1": {RxBytes: 1, TxBytes: 2}}
	st.BlkioStats.IoServiceBytesRecursive = []container.BlkioStatEntry{{Op: "Read", Value: 7}, {Op: "write", Value: 9}, {Op: "Total", Value: 16}}
	return st
}

// mux frames a log stream like the daemon for containers without TTY.
func mux(stream byte, text string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(text))) //nolint:gosec // test data
	return append(h, text...)
}

// recorder collects what the backend sends.
type recorder struct {
	mu   sync.Mutex
	msgs []agentproto.Envelope
}

func (r *recorder) Send(_ context.Context, typ, ref string, payload any) error {
	e, err := agentproto.New(typ, ref, payload)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.msgs = append(r.msgs, e)
	r.mu.Unlock()
	return nil
}

func (r *recorder) of(typ string) []agentproto.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []agentproto.Envelope
	for _, e := range r.msgs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func newTestDocker(t *testing.T) (*Docker, *fakeDocker, *lockedBuf) {
	t.Helper()
	fd := &fakeDocker{events: make(chan events.Message, 4), items: []container.Summary{
		{ID: "id-mc", Names: []string{"/mc"}, Image: "itzg/minecraft-server", State: "running",
			Labels: map[string]string{"rconductor.manage": "true"},
			Ports:  []container.PortSummary{{PrivatePort: 25575, PublicPort: 25575, Type: "tcp"}},
			Mounts: []container.MountPoint{{Type: "volume", Name: "mc-data"}, {Type: "bind", Source: "/etc/hosts"}}},
		{ID: "id-db", Names: []string{"/db"}, Image: "postgres", State: "running", Labels: map[string]string{}},
		{ID: "id-other", Names: []string{"/other"}, Image: "x", State: "running", Labels: map[string]string{"rconductor.manage": "false"}},
		// The label of the test versions up to 0.9 allows nothing any more.
		{ID: "id-old", Names: []string{"/old"}, Image: "x", State: "running", Labels: map[string]string{"rcon.manage": "true"}},
	}}
	logs := &lockedBuf{}
	log := logging.New(logs, "agent", config.Log{Format: "json"}).Root()
	return newDocker(fd, config.Agent{LabelKey: "rconductor.manage", LabelValue: "true"}, log), fd, logs
}

func order(t *testing.T, d *Docker, typ string, payload any) (agentproto.Result, *recorder) {
	t.Helper()
	e, err := agentproto.New(typ, "", payload)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	d.Handle(context.Background(), e, rec)
	res := rec.of(agentproto.TypeResult)
	if len(res) != 1 || res[0].Ref != e.ID {
		t.Fatalf("%s: results %+v", typ, res)
	}
	r, _ := agentproto.Payload[agentproto.Result](res[0])
	return r, rec
}

// TestSnapshotOnlyLabeled covers AGT-002: only containers with the label
// appear, even when the daemon lists more.
func TestSnapshotOnlyLabeled(t *testing.T) {
	d, _, _ := newTestDocker(t)
	rec := &recorder{}
	if err := d.snapshot(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	snap, _ := agentproto.Payload[agentproto.Snapshot](rec.of(agentproto.TypeSnapshot)[0])
	if len(snap.Containers) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	c := snap.Containers[0]
	if c.Name != "mc" || c.State != "running" || len(c.Ports) != 1 || c.Ports[0].Private != 25575 ||
		len(c.Volumes) != 1 || c.Volumes[0] != "mc-data" || c.RestartCount != 2 || c.StartedAt.IsZero() {
		t.Errorf("container = %+v", c)
	}
}

// TestOrdersCheckedAgain covers AGT-002, AGT-004 and Design 1.6: an order
// for a container without the label is forbidden even when the panel sends
// it; names are checked; unknown orders are unsupported; the rate per
// container is bounded.
func TestOrdersCheckedAgain(t *testing.T) {
	d, fd, logs := newTestDocker(t)
	for _, name := range []string{"db", "other", "nope"} {
		if r, _ := order(t, d, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: name, Action: "restart"}); r.Code != agentproto.CodeForbidden {
			t.Errorf("%s: %+v", name, r)
		}
	}
	logtest.Wait(t, logs, `"event":"agent.forbidden"`)
	for _, bad := range []agentproto.ContainerAction{{Container: "mc|db", Action: "restart"}, {Container: "", Action: "stop"}, {Container: "mc", Action: "remove"}} {
		if r, _ := order(t, d, agentproto.TypeContainerAction, bad); r.Code != agentproto.CodeInvalid {
			t.Errorf("%+v: %+v", bad, r)
		}
	}
	for _, typ := range []string{agentproto.TypeVersionDetect, agentproto.TypeVolumesSize, "container.exec"} {
		if r, _ := order(t, d, typ, map[string]string{"container": "mc"}); r.Code != agentproto.CodeUnsupported {
			t.Errorf("%s: %+v", typ, r)
		}
	}
	if len(fd.actions) != 0 {
		t.Fatalf("docker called: %v", fd.actions)
	}

	for i, action := range []string{"restart", "stop", "start", "restart"} {
		r, _ := order(t, d, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: "mc", Action: action})
		want := agentproto.CodeOK
		if i == 3 {
			want = agentproto.CodeRateLimited
		}
		if r.Code != want {
			t.Errorf("order %d (%s): %+v, want %s", i, action, r, want)
		}
	}
	if strings.Join(fd.actions, ",") != "restart id-mc,stop id-mc,start id-mc" {
		t.Errorf("actions = %v", fd.actions)
	}
	if logtest.WaitCount(t, logs, `"event":"docker.action"`, 3) != 3 {
		t.Errorf("docker.action events:\n%s", logs.String())
	}
}

// TestLogsTail covers the log part of AGT-003 with SEC-007: the stream is
// demultiplexed, cleaned and bounded.
func TestLogsTail(t *testing.T) {
	d, fd, _ := newTestDocker(t)
	fd.logs = append(mux(1, "line 1\n\x1b[31mline 2\n"), mux(2, "error line\n")...)
	r, rec := order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: "mc", Lines: 50})
	if r.Code != agentproto.CodeOK {
		t.Fatalf("result = %+v", r)
	}
	ll, _ := agentproto.Payload[agentproto.LogLines](rec.of(agentproto.TypeLogLines)[0])
	if strings.Join(ll.Lines, "|") != "line 1|[31mline 2|error line" || ll.Truncated {
		t.Errorf("lines = %q %v", ll.Lines, ll.Truncated)
	}
	if r, _ := order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: "db", Lines: 5}); r.Code != agentproto.CodeForbidden {
		t.Errorf("logs of an unlabeled container: %+v", r)
	}
	var many strings.Builder
	for range 700 {
		many.WriteString("x\n")
	}
	fd.tty, fd.logs = true, []byte(many.String())
	d.limiter = newOrderLimiter(100, 100)
	_, rec = order(t, d, agentproto.TypeLogsTail, agentproto.LogsTail{Container: "mc", Lines: 10000})
	ll, _ = agentproto.Payload[agentproto.LogLines](rec.of(agentproto.TypeLogLines)[0])
	if len(ll.Lines) != agentproto.MaxLogLines || !ll.Truncated {
		t.Errorf("bounded lines: %d %v", len(ll.Lines), ll.Truncated)
	}
}

// TestFollowLogs covers UI-009 on the agent: logs.follow streams lines,
// logs.stop ends it (logs.follow.start and logs.follow.stop).
func TestFollowLogs(t *testing.T) {
	d, fd, logs := newTestDocker(t)
	fd.tty, fd.logs = true, []byte("one\ntwo\n")
	e, _ := agentproto.New(agentproto.TypeLogsFollow, "", agentproto.LogsFollow{Container: "mc"})
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Handle(ctx, e, rec)
	waitFor(t, "lines", func() bool { return len(rec.of(agentproto.TypeLogLines)) > 0 })
	ll, _ := agentproto.Payload[agentproto.LogLines](rec.of(agentproto.TypeLogLines)[0])
	if strings.Join(ll.Lines, ",") != "one,two" || rec.of(agentproto.TypeLogLines)[0].Ref != e.ID {
		t.Errorf("follow = %+v", ll)
	}
	stop, _ := agentproto.New(agentproto.TypeLogsStop, "", agentproto.LogsFollow{Container: "mc"})
	d.Handle(ctx, stop, rec)
	for _, ev := range []string{"logs.follow.start", "logs.follow.stop"} {
		logtest.Wait(t, logs, `"event":"`+ev+`"`)
	}
}

func TestMetrics(t *testing.T) {
	d, _, _ := newTestDocker(t)
	m, err := d.collect(context.Background())
	if err != nil || len(m.Containers) != 1 {
		t.Fatalf("metrics = %+v %v", m, err)
	}
	got := m.Containers[0]
	want := agentproto.Metric{Name: "mc", CPUPercent: 80, MemBytes: 400, MemLimit: 2000, NetRx: 11, NetTx: 22, BlkRead: 7, BlkWrite: 9}
	if got != want {
		t.Errorf("metric = %+v, want %+v", got, want)
	}
}

// TestEvents: a status change of an allowed container is forwarded; a
// container that left the label set is reported gone; others never.
func TestEvents(t *testing.T) {
	d, fd, _ := newTestDocker(t)
	d.metrics = time.Hour
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, rec) }()
	fd.events <- events.Message{Type: "container", Action: "die", Actor: events.Actor{ID: "id-mc", Attributes: map[string]string{"name": "mc"}}}
	fd.events <- events.Message{Type: "container", Action: "exec_start", Actor: events.Actor{ID: "id-mc"}}
	fd.events <- events.Message{Type: "container", Action: "destroy", Actor: events.Actor{ID: "id-gone", Attributes: map[string]string{"name": "old"}}}
	waitFor(t, "events", func() bool { return len(rec.of(agentproto.TypeContainerEvent)) == 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	evs := rec.of(agentproto.TypeContainerEvent)
	first, _ := agentproto.Payload[agentproto.ContainerEvent](evs[0])
	second, _ := agentproto.Payload[agentproto.ContainerEvent](evs[1])
	if first.Container.Name != "mc" || first.Gone || second.Container.Name != "old" || !second.Gone {
		t.Errorf("events = %+v %+v", first, second)
	}
	if len(rec.of(agentproto.TypeSnapshot)) != 1 {
		t.Error("no snapshot first")
	}
}

// TestK3FollowOutsideTheOrderLimit covers #155: switching between console
// and logs does not use up the actions of a container.
func TestK3FollowOutsideTheOrderLimit(t *testing.T) {
	d, _, _ := newTestDocker(t)
	for i := range 6 {
		r, _ := order(t, d, agentproto.TypeLogsFollow, agentproto.LogsFollow{Container: "mc"})
		if r.Code != agentproto.CodeOK {
			t.Fatalf("follow %d = %s", i, r.Code)
		}
		if r, _ := order(t, d, agentproto.TypeLogsStop, agentproto.LogsFollow{Container: "mc"}); r.Code != agentproto.CodeOK {
			t.Fatalf("stop %d = %s", i, r.Code)
		}
	}
}

// TestC4FollowHasItsOwnLimit: switching often stays free of the actions but
// does not go without bound. Failed before the fix: no limit at all.
func TestC4FollowHasItsOwnLimit(t *testing.T) {
	d, _, _ := newTestDocker(t)
	follow := func() string {
		r, _ := order(t, d, agentproto.TypeLogsFollow, agentproto.LogsFollow{Container: "mc"})
		_, _ = order(t, d, agentproto.TypeLogsStop, agentproto.LogsFollow{Container: "mc"})
		return r.Code
	}
	for i := range 20 {
		if code := follow(); code != agentproto.CodeOK {
			t.Fatalf("follow %d = %s", i, code)
		}
	}
	if code := follow(); code != agentproto.CodeRateLimited {
		t.Errorf("follow over the burst = %s", code)
	}
}

// stallingSender holds a send until told, then notes whether its context
// still stood.
type stallingSender struct {
	entered, release chan struct{}
	err              chan error
}

func (s *stallingSender) Send(ctx context.Context, _, _ string, _ any) error {
	s.entered <- struct{}{}
	<-s.release
	s.err <- ctx.Err()
	return nil
}

// stringStream is a log stream that ends when closed.
type stringStream struct {
	io.Reader
	done chan struct{}
}

func (s stringStream) Close() error { close(s.done); return nil }

// TestLogsStopDuringASendKeepsTheSession: a stop that comes while lines are
// on their way must not cancel the write. coder/websocket closes the whole
// connection when the context of a write ends during it, so the agent lost
// the panel (TestAgentThroughSocketProxy in CI). Failed before the fix: the
// send ran under the context of the follow.
func TestLogsStopDuringASendKeepsTheSession(t *testing.T) {
	d, _, _ := newTestDocker(t)
	session, end := context.WithCancel(context.Background())
	defer end()
	fctx, cancel := context.WithCancel(session)
	d.mu.Lock()
	d.follows["mc"] = cancel
	d.mu.Unlock()
	pr, pw := io.Pipe()
	s := &stallingSender{entered: make(chan struct{}), release: make(chan struct{}), err: make(chan error, 1)}
	go d.pump(fctx, session, stringStream{Reader: pr, done: make(chan struct{})}, "mc", "ref", s)
	go func() { _, _ = io.WriteString(pw, "a line\n") }()
	<-s.entered
	d.stopFollow("mc")
	close(s.release)
	if err := <-s.err; err != nil {
		t.Errorf("the send lost its context: %v", err)
	}
	_ = pw.Close()
}
