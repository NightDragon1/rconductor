// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// dockerAPI is the part of the Docker API the agent uses: no create,
// remove, exec, image or volume calls exist here (AGT-004).
type dockerAPI interface {
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(ctx context.Context, id string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStart(ctx context.Context, id string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(ctx context.Context, id string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRestart(ctx context.Context, id string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	ContainerStats(ctx context.Context, id string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	ContainerLogs(ctx context.Context, id string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	Events(ctx context.Context, options client.EventsListOptions) client.EventsResult
}

// Timing of the Docker backend (Design 1.5, Gesamtkonzept 4.6).
const (
	MetricsInterval = 10 * time.Second
	followFlush     = 500 * time.Millisecond
	maxFollows      = 16
)

// Docker is the backend for Docker hosts; it sees only containers with the
// label (AGT-002) and checks it again for every order (Design 1.6).
type Docker struct {
	api        dockerAPI
	log        *slog.Logger
	labelKey   string
	labelValue string
	limiter    *orderLimiter
	// followLimiter is for logs.follow: switching between console and logs
	// must not use up the actions of a container (#155), and still costs
	// calls to Docker (C4).
	followLimiter *orderLimiter
	metrics       time.Duration
	maxFollows    int

	mu      sync.Mutex
	follows map[string]context.CancelFunc
}

// NewDocker connects to the Docker daemon at RCD_DOCKER_HOST.
func NewDocker(cfg config.Agent, log *slog.Logger) (*Docker, error) {
	c, err := client.New(client.WithHost(cfg.DockerHost))
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return newDocker(c, cfg, log), nil
}

func newDocker(api dockerAPI, cfg config.Agent, log *slog.Logger) *Docker {
	orders, burst, metrics, follows := 10, 3, MetricsInterval, maxFollows
	if cfg.OrdersPerMinute > 0 {
		orders = cfg.OrdersPerMinute
	}
	if cfg.OrdersBurst > 0 {
		burst = cfg.OrdersBurst
	}
	if cfg.MetricsInterval > 0 {
		metrics = cfg.MetricsInterval
	}
	if cfg.MaxFollows > 0 {
		follows = cfg.MaxFollows
	}
	followRate, followBurst := 60, 20
	if cfg.FollowsPerMinute > 0 {
		followRate = cfg.FollowsPerMinute
	}
	if cfg.FollowsBurst > 0 {
		followBurst = cfg.FollowsBurst
	}
	return &Docker{
		api: api, log: log, labelKey: cfg.LabelKey, labelValue: cfg.LabelValue, maxFollows: follows,
		limiter: newOrderLimiter(orders, burst), followLimiter: newOrderLimiter(followRate, followBurst), metrics: metrics, follows: map[string]context.CancelFunc{},
	}
}

// Name implements Backend.
func (d *Docker) Name() string { return "docker" }

// Capabilities implements Backend; version.detect and volumes.size follow
// in R2.
func (d *Docker) Capabilities() []string {
	return []string{agentproto.CapContainerAction, agentproto.CapLogs, agentproto.CapMetrics, agentproto.CapRestartInfo}
}

func (d *Docker) labelFilter() client.Filters {
	return client.Filters{}.Add("label", d.labelKey+"="+d.labelValue)
}

func (d *Docker) list(ctx context.Context, f client.Filters) ([]container.Summary, error) {
	r, err := d.api.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	out := r.Items[:0]
	for _, s := range r.Items {
		// The daemon filters, the agent checks again.
		if s.Labels[d.labelKey] == d.labelValue {
			out = append(out, s)
		}
	}
	return out, nil
}

func toContainer(s container.Summary) agentproto.Container {
	c := agentproto.Container{ID: s.ID, Image: s.Image, State: string(s.State)}
	if len(s.Names) > 0 {
		c.Name = strings.TrimPrefix(s.Names[0], "/")
	}
	for _, p := range s.Ports {
		c.Ports = append(c.Ports, agentproto.Port{Private: int(p.PrivatePort), Public: int(p.PublicPort), Proto: p.Type})
	}
	for _, m := range s.Mounts {
		if m.Type == "volume" && m.Name != "" {
			c.Volumes = append(c.Volumes, m.Name)
		}
	}
	c.Clean()
	return c
}

// withStart adds start time and restart count from an inspect; without
// them the panel shows neither (revision 2.4 candidate 2).
func (d *Docker) withStart(ctx context.Context, c agentproto.Container) agentproto.Container {
	ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := d.api.ContainerInspect(ictx, c.ID, client.ContainerInspectOptions{})
	if err != nil {
		return c
	}
	c.RestartCount = r.Container.RestartCount
	if r.Container.State != nil {
		if t, err := time.Parse(time.RFC3339Nano, r.Container.State.StartedAt); err == nil && t.Year() > 1 {
			c.StartedAt = t.UTC()
		}
	}
	return c
}

// resolve finds an allowed container by name; a container without the
// label does not exist for the agent (revision 2.3 candidate 1).
func (d *Docker) resolve(ctx context.Context, name string) (container.Summary, error) {
	items, err := d.list(ctx, d.labelFilter().Add("name", "^/"+name+"$"))
	if err != nil {
		return container.Summary{}, err
	}
	for _, s := range items {
		if len(s.Names) > 0 && strings.TrimPrefix(s.Names[0], "/") == name {
			return s, nil
		}
	}
	return container.Summary{}, errForbidden
}

var errForbidden = errors.New("container not allowed")

// Run implements Backend: snapshot, then events and metrics.
func (d *Docker) Run(ctx context.Context, out Sender) error {
	if err := d.snapshot(ctx, out); err != nil {
		return err
	}
	go d.metricsLoop(ctx, out)
	defer d.stopFollows()
	for {
		err := d.events(ctx, out)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // shutting down is no error
		}
		d.log.Warn("docker events ended; resubscribing", slog.Any("err", err))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
		if err := d.snapshot(ctx, out); err != nil {
			d.log.Warn("snapshot failed", slog.Any("err", err))
		}
	}
}

func (d *Docker) snapshot(ctx context.Context, out Sender) error {
	items, err := d.list(ctx, d.labelFilter())
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	if len(items) > agentproto.MaxContainers {
		items = items[:agentproto.MaxContainers]
	}
	snap := agentproto.Snapshot{Containers: make([]agentproto.Container, 0, len(items))}
	for _, s := range items {
		snap.Containers = append(snap.Containers, d.withStart(ctx, toContainer(s)))
	}
	return out.Send(ctx, agentproto.TypeSnapshot, "", snap)
}

// events forwards status changes of allowed containers until the stream
// ends.
func (d *Docker) events(ctx context.Context, out Sender) error {
	ectx, cancel := context.WithCancel(ctx)
	defer cancel()
	res := d.api.Events(ectx, client.EventsListOptions{Filters: d.labelFilter().Add("type", "container")})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-res.Err:
			return err
		case m := <-res.Messages:
			d.event(ctx, out, m)
		}
	}
}

// statusActions are the events that change what the panel shows.
var statusActions = map[events.Action]bool{
	"create": true, "start": true, "stop": true, "die": true, "kill": true, "restart": true,
	"pause": true, "unpause": true, "destroy": true, "rename": true,
}

func (d *Docker) event(ctx context.Context, out Sender, m events.Message) {
	if !statusActions[m.Action] {
		return
	}
	name := strings.TrimPrefix(m.Actor.Attributes["name"], "/")
	items, err := d.list(ctx, d.labelFilter().Add("id", m.Actor.ID))
	if err != nil {
		d.log.Warn("container event not resolved", slog.Any("err", err))
		return
	}
	ev := agentproto.ContainerEvent{}
	if len(items) == 1 {
		ev.Container = d.withStart(ctx, toContainer(items[0]))
	} else {
		ev.Container = agentproto.Container{ID: m.Actor.ID, Name: name}
		ev.Container.Clean()
		ev.Gone = true
	}
	if err := out.Send(ctx, agentproto.TypeContainerEvent, "", ev); err != nil {
		d.log.Debug("container event not sent", slog.Any("err", err))
	}
}

func (d *Docker) metricsLoop(ctx context.Context, out Sender) {
	t := time.NewTicker(d.metrics)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m, err := d.collect(ctx)
			if err != nil {
				d.log.Warn("metrics not collected", slog.Any("err", err))
				continue
			}
			if err := out.Send(ctx, agentproto.TypeMetrics, "", m); err != nil {
				d.log.Debug("metrics not sent", slog.Any("err", err))
			}
		}
	}
}

// collect reads one sample of every running allowed container (AGT-003).
func (d *Docker) collect(ctx context.Context) (agentproto.Metrics, error) {
	items, err := d.list(ctx, d.labelFilter().Add("status", "running"))
	if err != nil {
		return agentproto.Metrics{}, err
	}
	m := agentproto.Metrics{Containers: []agentproto.Metric{}}
	for _, s := range items {
		c := toContainer(s)
		st, err := d.stats(ctx, s.ID)
		if err != nil {
			d.log.Debug("stats not read", slog.String("container", c.Name), slog.Any("err", err))
			continue
		}
		mt := metricOf(st)
		mt.Name = c.Name
		m.Containers = append(m.Containers, mt)
		if len(m.Containers) == agentproto.MaxContainers {
			break
		}
	}
	return m, nil
}

func (d *Docker) stats(ctx context.Context, id string) (container.StatsResponse, error) {
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := d.api.ContainerStats(sctx, id, client.ContainerStatsOptions{IncludePreviousSample: true})
	if err != nil {
		return container.StatsResponse{}, err
	}
	defer func() { _ = r.Body.Close() }()
	var st container.StatsResponse
	if err := jsonDecode(io.LimitReader(r.Body, 1<<20), &st); err != nil {
		return container.StatsResponse{}, err
	}
	return st, nil
}

// metricOf turns a Docker sample into the numbers of Design 1.5: CPU as
// percent of one core times the cores, memory without the page cache,
// network and block I/O as totals.
func metricOf(st container.StatsResponse) agentproto.Metric {
	var m agentproto.Metric
	cpu := float64(st.CPUStats.CPUUsage.TotalUsage) - float64(st.PreCPUStats.CPUUsage.TotalUsage)
	sys := float64(st.CPUStats.SystemUsage) - float64(st.PreCPUStats.SystemUsage)
	cores := float64(st.CPUStats.OnlineCPUs)
	if cores == 0 {
		cores = float64(len(st.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpu > 0 && sys > 0 {
		m.CPUPercent = cpu / sys * cores * 100
	}
	m.MemBytes = st.MemoryStats.Usage
	if cache, ok := st.MemoryStats.Stats["inactive_file"]; ok && cache < m.MemBytes {
		m.MemBytes -= cache
	}
	m.MemLimit = st.MemoryStats.Limit
	for _, n := range st.Networks {
		m.NetRx += n.RxBytes
		m.NetTx += n.TxBytes
	}
	for _, b := range st.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			m.BlkRead += b.Value
		case "write":
			m.BlkWrite += b.Value
		}
	}
	return m
}

// Handle implements Backend: every order is checked against the label and
// the rate per container before it reaches Docker (Design 1.6).
func (d *Docker) Handle(ctx context.Context, e agentproto.Envelope, out Sender) {
	code, msg := d.handle(ctx, e, out)
	if err := out.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: code, Message: msg}); err != nil {
		d.log.Debug("result not sent", slog.Any("err", err))
	}
}

func (d *Docker) handle(ctx context.Context, e agentproto.Envelope, out Sender) (code, msg string) {
	var name string
	switch e.Type {
	case agentproto.TypeContainerAction:
		a, err := agentproto.Payload[agentproto.ContainerAction](e)
		if err != nil || !validName(a.Container) {
			return agentproto.CodeInvalid, ""
		}
		name = a.Container
		if a.Action != agentproto.ActionStart && a.Action != agentproto.ActionStop && a.Action != agentproto.ActionRestart {
			return agentproto.CodeInvalid, "unknown action"
		}
		s, code := d.allow(ctx, name, e.Type)
		if code != "" {
			return code, ""
		}
		return d.action(ctx, s, name, a.Action)
	case agentproto.TypeLogsTail:
		t, err := agentproto.Payload[agentproto.LogsTail](e)
		if err != nil || !validName(t.Container) || t.Lines < 1 {
			return agentproto.CodeInvalid, ""
		}
		s, code := d.allow(ctx, t.Container, e.Type)
		if code != "" {
			return code, ""
		}
		return d.tail(ctx, s, t.Container, min(t.Lines, agentproto.MaxLogLines), e.ID, out)
	case agentproto.TypeLogsFollow:
		f, err := agentproto.Payload[agentproto.LogsFollow](e)
		if err != nil || !validName(f.Container) {
			return agentproto.CodeInvalid, ""
		}
		s, code := d.allow(ctx, f.Container, e.Type)
		if code != "" {
			return code, ""
		}
		return d.follow(ctx, s, f.Container, e.ID, out)
	case agentproto.TypeLogsStop:
		f, err := agentproto.Payload[agentproto.LogsFollow](e)
		if err != nil || !validName(f.Container) {
			return agentproto.CodeInvalid, ""
		}
		d.stopFollow(f.Container)
		return agentproto.CodeOK, ""
	}
	// version.detect and volumes.size come with R2; nothing else exists.
	d.log.Warn("order not supported", logging.Event(logging.AgentUnsupported), slog.String("type", e.Type))
	return agentproto.CodeUnsupported, ""
}

func validName(n string) bool {
	if n == "" || len(n) > 255 {
		return false
	}
	for _, r := range n {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// allow checks the label and the rate; the code is empty when allowed.
func (d *Docker) allow(ctx context.Context, name, typ string) (container.Summary, string) {
	s, err := d.resolve(ctx, name)
	switch {
	case errors.Is(err, errForbidden):
		d.log.Warn("order for a container without the label", logging.Event(logging.AgentForbidden),
			slog.String("container", name), slog.String("type", typ))
		return s, agentproto.CodeForbidden
	case err != nil:
		d.log.Warn("container not resolved", slog.String("container", name), slog.Any("err", err))
		return s, agentproto.CodeNotFound
	}
	limiter := d.limiter
	if typ == agentproto.TypeLogsFollow {
		limiter = d.followLimiter
	}
	if !limiter.allow(name) {
		return s, agentproto.CodeRateLimited
	}
	return s, ""
}

func (d *Docker) action(ctx context.Context, s container.Summary, name, action string) (code, msg string) {
	actx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var err error
	switch action {
	case agentproto.ActionStart:
		_, err = d.api.ContainerStart(actx, s.ID, client.ContainerStartOptions{})
	case agentproto.ActionStop:
		_, err = d.api.ContainerStop(actx, s.ID, client.ContainerStopOptions{})
	case agentproto.ActionRestart:
		_, err = d.api.ContainerRestart(actx, s.ID, client.ContainerRestartOptions{})
	}
	result := agentproto.CodeOK
	if err != nil {
		result = agentproto.CodeError
		if errors.Is(actx.Err(), context.DeadlineExceeded) {
			result = agentproto.CodeTimeout
		}
	}
	d.log.Info("container action", logging.Event(logging.DockerAction), slog.String("container", name),
		slog.String("action", action), slog.String("result", result))
	if err != nil {
		return result, "docker refused the action"
	}
	return agentproto.CodeOK, ""
}

// logStream reads container logs; containers without TTY multiplex stdout
// and stderr.
func (d *Docker) logStream(ctx context.Context, s container.Summary, opts client.ContainerLogsOptions) (io.ReadCloser, error) {
	opts.ShowStdout, opts.ShowStderr = true, true
	r, err := d.api.ContainerLogs(ctx, s.ID, opts)
	if err != nil {
		return nil, err
	}
	insp, err := d.api.ContainerInspect(ctx, s.ID, client.ContainerInspectOptions{})
	if err == nil && insp.Container.Config != nil && insp.Container.Config.Tty {
		return r, nil
	}
	pr, pw := io.Pipe()
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, r)
		_ = r.Close()
		_ = pw.CloseWithError(err)
	}()
	return pr, nil
}

func (d *Docker) tail(ctx context.Context, s container.Summary, name string, n int, ref string, out Sender) (code, msg string) {
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := d.logStream(tctx, s, client.ContainerLogsOptions{Tail: strconv.Itoa(n)})
	if err != nil {
		return agentproto.CodeError, "logs not readable"
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(r, int64(agentproto.MaxLogLines)*int64(agentproto.MaxLogLine)))
	if err != nil && !errors.Is(err, io.EOF) {
		return agentproto.CodeError, "logs not readable"
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(b) == 0 {
		lines = nil
	}
	lines, cut := agentproto.LimitLines(lines)
	if err := out.Send(ctx, agentproto.TypeLogLines, ref, agentproto.LogLines{Container: name, Lines: lines, Truncated: cut}); err != nil {
		return agentproto.CodeError, ""
	}
	return agentproto.CodeOK, ""
}

// follow streams new log lines until logs.stop or the session ends (UI-009:
// only while someone views them).
func (d *Docker) follow(ctx context.Context, s container.Summary, name, ref string, out Sender) (code, msg string) {
	d.mu.Lock()
	if _, ok := d.follows[name]; ok {
		d.mu.Unlock()
		return agentproto.CodeOK, "" // already following
	}
	if len(d.follows) >= d.maxFollows {
		d.mu.Unlock()
		return agentproto.CodeRateLimited, "too many followed logs"
	}
	fctx, cancel := context.WithCancel(ctx)
	d.follows[name] = cancel
	d.mu.Unlock()
	r, err := d.logStream(fctx, s, client.ContainerLogsOptions{Follow: true, Tail: "0"})
	if err != nil {
		d.stopFollow(name)
		return agentproto.CodeError, "logs not readable"
	}
	d.log.Info("following container logs", logging.Event(logging.LogsFollowStart), slog.String("container", name))
	go d.pump(fctx, ctx, r, name, ref, out)
	return agentproto.CodeOK, ""
}

// pump sends the lines of a follow until ctx ends. Sends run under session,
// not ctx: coder/websocket closes the connection when the context of a write
// ends during it, so a logs.stop in the middle of a send would end the session.
func (d *Docker) pump(ctx, session context.Context, r io.ReadCloser, name, ref string, out Sender) {
	defer func() {
		_ = r.Close()
		d.stopFollow(name)
	}()
	lines := make(chan string, agentproto.MaxLogLines)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	t := time.NewTicker(followFlush)
	defer t.Stop()
	var batch []string
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ls, cut := agentproto.LimitLines(batch)
		batch = nil
		_ = out.Send(session, agentproto.TypeLogLines, ref, agentproto.LogLines{Container: name, Lines: ls, Truncated: cut})
	}
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-lines:
			if !ok {
				flush()
				return
			}
			batch = append(batch, l)
			if len(batch) >= agentproto.MaxLogLines {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

func (d *Docker) stopFollow(name string) {
	d.mu.Lock()
	cancel, ok := d.follows[name]
	delete(d.follows, name)
	d.mu.Unlock()
	if ok {
		cancel()
		d.log.Info("stopped following container logs", logging.Event(logging.LogsFollowStop), slog.String("container", name))
	}
}

func (d *Docker) stopFollows() {
	d.mu.Lock()
	names := make([]string, 0, len(d.follows))
	for n := range d.follows {
		names = append(names, n)
	}
	d.mu.Unlock()
	for _, n := range names {
		d.stopFollow(n)
	}
}

// orderLimiter is a token bucket per container (Design 1.6, revision 2.3
// candidate 2: 10 per minute, 3 at once).
type orderLimiter struct {
	perMinute, burst float64
	now              func() time.Time

	mu      sync.Mutex
	buckets map[string]*tokens
}

type tokens struct {
	n    float64
	last time.Time
}

func newOrderLimiter(perMinute, burst int) *orderLimiter {
	return &orderLimiter{perMinute: float64(perMinute), burst: float64(burst), now: time.Now, buckets: map[string]*tokens{}}
}

func (l *orderLimiter) allow(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[name]
	if b == nil {
		if len(l.buckets) >= 10000 {
			clear(l.buckets)
		}
		b = &tokens{n: l.burst, last: now}
		l.buckets[name] = b
	}
	b.n = min(l.burst, b.n+now.Sub(b.last).Minutes()*l.perMinute)
	b.last = now
	if b.n < 1 {
		return false
	}
	b.n--
	return true
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }
