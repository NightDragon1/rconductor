// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
	builtinprofiles "github.com/NightDragon1/rconductor/profiles"
)

// Pinned images of the agent tests.
const (
	alpineImage      = "alpine@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f"
	socketProxyImage = "lscr.io/linuxserver/socket-proxy@sha256:4e5e59efd3338205561c54262581992036e5a36ccca97d487b1d4ec41f9adc58"
	label            = "rcon.manage"
)

func agentImage() string {
	if img := os.Getenv("RCD_AGENT_IMAGE"); img != "" {
		return img
	}
	return "rconductor-agent:dev"
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// panelSide is the agent part of a panel in this process: agent
// administration, agent hub and cache.
type panelSide struct {
	t     *testing.T
	st    *store.Store
	svc   *agents.Service
	hub   *agenthub.Hub
	data  *cache.AgentData
	addr  string
	logs  *syncBuf
	admin users.Request
	log   *slog.Logger
}

func newPanelSide(t *testing.T, listen string) *panelSide {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := users.EnsureAuditDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	logs := &syncBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("agent")
	key, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.PanelKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	p := &panelSide{t: t, st: st, logs: logs, log: log}
	p.svc = agents.NewService(st, log, "")
	p.hub = agenthub.New(key, log, agenthub.Options{Registry: p.svc})
	p.svc.SetHub(p.hub)
	p.data = cache.NewAgentData(log)
	p.hub.OnMessage(p.data.Receive)
	p.hub.OnDisconnect(p.data.Disconnected)
	hctx, cancel := context.WithCancel(ctx)
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- p.hub.Serve(hctx, listen, func(a net.Addr) { ready <- a }) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("hub: %v", err)
		}
	})
	p.addr = (<-ready).String()
	adminID := storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	p.admin = users.Request{Actor: perm.Actor{ID: adminID, Role: perm.RCONAdmin}, Pseudonym: "admin", RequestID: "it"}
	return p
}

func (p *panelSide) token() string {
	p.t.Helper()
	tok, err := p.svc.CreateToken(context.Background(), p.admin)
	if err != nil {
		p.t.Fatal(err)
	}
	return tok.Token
}

// connectedAgent waits for the one agent to be connected and returns its ID.
func (p *panelSide) connectedAgent() string {
	p.t.Helper()
	var id string
	waitUntil(p.t, 60*time.Second, "agent connected", func() bool {
		list, _ := p.svc.List(context.Background(), p.admin)
		if len(list) == 1 && list[0].Connected {
			id = list[0].ID
		}
		return id != ""
	})
	return id
}

func (p *panelSide) request(agentID, typ string, payload any) agentproto.Result {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	r, err := p.hub.Request(ctx, agentID, typ, payload)
	if err != nil {
		p.t.Fatalf("%s: %v", typ, err)
	}
	return r
}

func waitUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startTicker runs a container that prints a line every second.
func startTicker(t *testing.T, name string, labels map[string]string) *testcontainers.DockerContainer {
	t.Helper()
	ctr, err := testcontainers.Run(context.Background(), alpineImage,
		testcontainers.WithName(name),
		testcontainers.WithLabels(labels),
		testcontainers.WithCmd("sh", "-c", "while true; do echo tick; sleep 1; done"),
		testcontainers.WithWaitStrategy(wait.ForLog("tick")),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	return ctr
}

// runAgent runs an agent in this process with the real Docker backend.
func runAgent(t *testing.T, p *panelSide, dockerHost string) *syncBuf {
	t.Helper()
	logs := &syncBuf{}
	cfg := config.Agent{
		PairingToken: p.token(), PanelAddr: p.addr, Name: "it-host", LabelKey: label, LabelValue: "true",
		DockerHost: dockerHost, SecretsDir: t.TempDir(), Log: config.Log{Format: "json"},
	}
	log := logging.New(logs, "agent", cfg.Log)
	d, err := agent.NewDocker(cfg, log.Module("docker"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := agent.New(cfg, log.Root(), d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("agent: %v", err)
		}
	})
	return logs
}

func dockerHost() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	return "unix:///var/run/docker.sock"
}

func uniq(prefix string) string { return fmt.Sprintf("rcd-it-%s-%d", prefix, time.Now().UnixNano()) }

func startedAt(t *testing.T, ctr *testcontainers.DockerContainer) string {
	t.Helper()
	info, err := ctr.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return info.State.StartedAt
}

// exerciseAgent runs the checks of AGT-002, AGT-003 and AGT-004 against a
// connected agent; dockerHost is how the agent reaches Docker.
func exerciseAgent(t *testing.T, p *panelSide, agentID, labeled, plain string, labeledCtr *testcontainers.DockerContainer) {
	t.Helper()
	// AGT-002: only the labeled container is visible.
	waitUntil(t, 30*time.Second, "snapshot", func() bool { _, ok := p.data.Container(agentID, labeled); return ok })
	if _, ok := p.data.Container(agentID, plain); ok {
		t.Errorf("container without label visible: %s", plain)
	}
	// ... and an order for the other one is forbidden, even when the
	// panel sends it.
	for _, action := range []string{"stop", "restart"} {
		if r := p.request(agentID, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: plain, Action: action}); r.Code != agentproto.CodeForbidden {
			t.Errorf("%s of the unlabeled container: %+v", action, r)
		}
	}
	// AGT-004: a manipulated panel cannot create, remove, exec or touch
	// images and volumes; unknown types are unsupported.
	for _, typ := range []string{"container.create", "container.remove", "container.exec", "image.pull", "volume.remove", "container.update"} {
		if r := p.request(agentID, typ, map[string]string{"container": labeled, "image": alpineImage, "cmd": "id"}); r.Code != agentproto.CodeUnsupported {
			t.Errorf("%s: %+v", typ, r)
		}
	}

	// AGT-003: logs, restart, stop, start, status, metrics.
	if r := p.request(agentID, agentproto.TypeLogsTail, agentproto.LogsTail{Container: labeled, Lines: 5}); r.Code != agentproto.CodeOK {
		t.Errorf("logs.tail: %+v", r)
	}
	waitUntil(t, 10*time.Second, "log lines", func() bool {
		l, _ := p.data.Logs(agentID, labeled)
		return slices.Contains(l, "tick")
	})
	// UI-009, SEC-010: live logs run until logs.stop, also through the proxy.
	if r := p.request(agentID, agentproto.TypeLogsFollow, agentproto.LogsFollow{Container: labeled}); r.Code != agentproto.CodeOK {
		t.Errorf("logs.follow: %+v", r)
	}
	_, seq := p.data.Logs(agentID, labeled)
	waitUntil(t, 10*time.Second, "live log lines", func() bool { _, s := p.data.Logs(agentID, labeled); return s > seq })
	if r := p.request(agentID, agentproto.TypeLogsStop, agentproto.LogsFollow{Container: labeled}); r.Code != agentproto.CodeOK {
		t.Errorf("logs.stop: %+v", r)
	}
	time.Sleep(500 * time.Millisecond) // lines already on their way
	_, seq = p.data.Logs(agentID, labeled)
	time.Sleep(3 * time.Second)
	if _, s := p.data.Logs(agentID, labeled); s != seq {
		t.Errorf("log lines keep coming after logs.stop: %d, then %d", seq, s)
	}

	before := startedAt(t, labeledCtr)
	if r := p.request(agentID, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: labeled, Action: "restart"}); r.Code != agentproto.CodeOK {
		t.Fatalf("restart: %+v", r)
	}
	if startedAt(t, labeledCtr) == before {
		t.Error("container not restarted")
	}
	if r := p.request(agentID, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: labeled, Action: "stop"}); r.Code != agentproto.CodeOK {
		t.Fatalf("stop: %+v", r)
	}
	waitUntil(t, 10*time.Second, "status exited", func() bool { c, _ := p.data.Container(agentID, labeled); return c.State == "exited" })
	if r := p.request(agentID, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: labeled, Action: "start"}); r.Code != agentproto.CodeOK {
		t.Fatalf("start: %+v", r)
	}
	waitUntil(t, 10*time.Second, "status running", func() bool { c, _ := p.data.Container(agentID, labeled); return c.State == "running" })
	waitUntil(t, 30*time.Second, "metrics", func() bool {
		m, ok := p.data.Metric(agentID, labeled)
		return ok && !m.Stale && m.MemBytes > 0 && m.CPUPercent > 0
	})
}

// TestAgentWithDocker covers AGT-002, AGT-003 and AGT-004 against the real
// Docker daemon.
func TestAgentWithDocker(t *testing.T) {
	labeled, plain := uniq("labeled"), uniq("plain")
	ctr := startTicker(t, labeled, map[string]string{label: "true"})
	startTicker(t, plain, map[string]string{label: "false"})
	p := newPanelSide(t, "127.0.0.1:0")
	logs := runAgent(t, p, dockerHost())
	agentID := p.connectedAgent()
	exerciseAgent(t, p, agentID, labeled, plain, ctr)
	for _, ev := range []string{"docker.action", "agent.forbidden", "agent.unsupported"} {
		logtest.Wait(t, logs, `"event":"`+ev+`"`)
	}
}

// TestAgentThroughSocketProxy covers SEC-010: with the socket proxy of the
// README in front of the socket, every function of AGT-003 works, and the
// proxy refuses what the agent never needs.
func TestAgentThroughSocketProxy(t *testing.T) {
	ctx := context.Background()
	proxy, err := testcontainers.Run(ctx, socketProxyImage,
		testcontainers.WithExposedPorts("2375/tcp"),
		testcontainers.WithEnv(map[string]string{
			"CONTAINERS": "1", "EVENTS": "1", "ALLOW_START": "1", "ALLOW_STOP": "1", "ALLOW_RESTARTS": "1", "ALLOW_LOGS": "1", "POST": "0",
		}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, "/var/run/docker.sock:/var/run/docker.sock:ro")
		}),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("2375/tcp")),
	)
	testcontainers.CleanupContainer(t, proxy)
	if err != nil {
		t.Fatalf("socket proxy: %v", err)
	}
	endpoint, err := proxy.PortEndpoint(ctx, "2375/tcp", "tcp")
	if err != nil {
		t.Fatal(err)
	}
	labeled, plain := uniq("proxy-labeled"), uniq("proxy-plain")
	ctr := startTicker(t, labeled, map[string]string{label: "true"})
	startTicker(t, plain, map[string]string{})
	p := newPanelSide(t, "127.0.0.1:0")
	runAgent(t, p, endpoint)
	exerciseAgent(t, p, p.connectedAgent(), labeled, plain, ctr)

	// The proxy itself refuses creating containers.
	dc, err := client.New(client.WithHost(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dc.Close() }()
	if _, err := dc.ContainerCreate(ctx, client.ContainerCreateOptions{Config: &container.Config{Image: alpineImage}}); err == nil {
		t.Error("the socket proxy allowed creating a container")
	}
}

// freePort returns a free local TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestRestartWithoutAgent covers AGT-009: without an agent the restart
// sends the profile's stop command; the container comes back with its
// restart policy unless-stopped and the panel reconnects.
func TestRestartWithoutAgent(t *testing.T) {
	ctx := context.Background()
	bin := filepath.Join(t.TempDir(), "rconserver")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./testdata/rconserver")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build test server: %v\n%s", err, out)
	}
	port := freePort(t)
	game, err := testcontainers.Run(ctx, alpineImage,
		testcontainers.WithName(uniq("rcon")),
		testcontainers.WithFiles(testcontainers.ContainerFile{HostFilePath: bin, ContainerFilePath: "/rconserver", FileMode: 0o755}),
		testcontainers.WithCmd("/rconserver"),
		testcontainers.WithExposedPorts("25575/tcp"),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}
			hc.PortBindings = network.PortMap{
				network.MustParsePort("25575/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: strconv.Itoa(port)}},
			}
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("RCON running")),
	)
	testcontainers.CleanupContainer(t, game)
	if err != nil {
		t.Fatalf("game container: %v", err)
	}

	st := storetest.New(t)
	logs := &syncBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("servers")
	if err := profiles.NewImporter(st, log, builtinprofiles.FS(), t.TempDir()).Sync(ctx); err != nil {
		t.Fatal(err)
	}
	checker := perm.NewChecker(st, log)
	k := make([]byte, 32)
	box, _ := secret.New(k)
	svc := servers.NewService(st, checker, box, log)
	conns := connmgr.New(rcon.Registry{"source-rcon": source.Adapter{}}, svc, log, connmgr.Options{ProbeInterval: 200 * time.Millisecond, MaxBackoff: time.Second})
	svc.SetConnections(conns)
	svc.SetActions(nil, conns)
	cctx, cancel := context.WithCancel(ctx)
	t.Cleanup(func() { cancel(); conns.Wait() })
	if err := conns.Start(cctx); err != nil {
		t.Fatal(err)
	}
	adminID := storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	admin := users.Request{Actor: perm.Actor{ID: adminID, Role: perm.RCONAdmin}, Pseudonym: "admin", RequestID: "it"}
	var profile string
	_ = st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'minecraft-java'`).Scan(&profile)
	name, host, pw := "mc", "127.0.0.1", logging.Secret("rconpw")
	srv, err := svc.Create(ctx, admin, servers.Input{Name: &name, ProfileID: &profile, Host: &host, Port: &port, Password: &pw})
	if err != nil {
		t.Fatal(err)
	}
	connected := func() bool { s, _ := conns.Status(srv.ID); return s == connmgr.Connected }
	waitUntil(t, 30*time.Second, "RCON connected", connected)
	uptime := func() string {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, _ := conns.Execute(cctx, srv.ID, "uptime")
		return out
	}
	first := uptime()

	r, err := svc.Action(ctx, admin, srv.ID, "restart")
	if err != nil || r.Via != "rcon" {
		t.Fatalf("restart = %+v %v", r, err)
	}
	waitUntil(t, 60*time.Second, "container restarted", func() bool {
		info, err := game.Inspect(ctx)
		return err == nil && info.RestartCount >= 1 && info.State.Running
	})
	waitUntil(t, 60*time.Second, "RCON back", func() bool { return connected() && uptime() != first && uptime() != "" })
	logtest.Wait(t, logs, `"event":"rcon.reconnect"`)
}

// TestAgentImage covers AGT-011 and AGT-001 for the image: non-root, no
// published port; it connects out to the panel and pairs itself.
func TestAgentImage(t *testing.T) {
	ctx := context.Background()
	p := newPanelSide(t, "127.0.0.1:0")
	var st syscall.Stat_t
	if err := syscall.Stat("/var/run/docker.sock", &st); err != nil {
		t.Skipf("no local docker socket: %v", err)
	}
	// Host network: the agent reaches the panel on 127.0.0.1 and still
	// cannot publish a port.
	ctr, err := testcontainers.Run(ctx, agentImage(),
		testcontainers.WithEnv(map[string]string{
			"RCD_PAIRING_TOKEN": p.token(), "RCD_PANEL_ADDR": p.addr, "RCD_LOG_FORMAT": "json",
		}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.NetworkMode = "host"
			hc.Binds = append(hc.Binds, "/var/run/docker.sock:/var/run/docker.sock")
			hc.GroupAdd = append(hc.GroupAdd, strconv.FormatUint(uint64(st.Gid), 10))
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("agent.handshake").WithStartupTimeout(60*time.Second)),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		if ctr != nil {
			if rc, lerr := ctr.Logs(ctx); lerr == nil {
				b, _ := io.ReadAll(rc)
				_ = rc.Close()
				t.Logf("agent log:\n%s", b)
			}
		}
		t.Logf("panel log:\n%s", p.logs.String())
		t.Fatalf("agent image: %v", err)
	}
	p.connectedAgent()
	info, err := ctr.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.User != nonRootUser || len(info.Config.ExposedPorts) != 0 || len(info.NetworkSettings.Ports) != 0 {
		t.Errorf("user %q, exposed %v, published %v", info.Config.User, info.Config.ExposedPorts, info.NetworkSettings.Ports)
	}
	rc, err := ctr.Logs(ctx)
	if err == nil {
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !strings.Contains(string(b), `"event":"key.created"`) {
			t.Errorf("agent log:\n%s", b)
		}
	}
}
