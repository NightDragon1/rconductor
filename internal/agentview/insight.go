// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store"
)

// Settings of the version search and the volume sizes (AGT-006, AGT-007,
// Design 5.6), with their defaults and smallest allowed values.
const (
	SettingVersionRetry   = "agent.version_retry"
	DefaultVersionRetry   = 30 * time.Second
	MinVersionRetry       = 5 * time.Second
	SettingVersionWindow  = "agent.version_window"
	DefaultVersionWindow  = 10 * time.Minute
	MinVersionWindow      = time.Minute
	SettingVolumeInterval = "agent.volume_interval"
	DefaultVolumeInterval = 2 * time.Hour
	MinVolumeInterval     = 10 * time.Minute
	SettingVolumeStale    = "agent.volume_stale"
	DefaultVolumeStale    = 6 * time.Hour
	MinVolumeStale        = 30 * time.Minute
)

// orderTimeout bounds one version.detect or volumes.size order.
const orderTimeout = 70 * time.Second

// EnsureDefaults stores the defaults of the settings unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	for key, d := range map[string]time.Duration{
		SettingVersionRetry: DefaultVersionRetry, SettingVersionWindow: DefaultVersionWindow,
		SettingVolumeInterval: DefaultVolumeInterval, SettingVolumeStale: DefaultVolumeStale,
	} {
		if _, err := st.EnsureDefault(ctx, key, d.String()); err != nil {
			return err
		}
	}
	return nil
}

// versionState is the search of one server's game version during one run
// of its container.
type versionState struct {
	key string // container, start, image and pattern
	// seen is when the panel first saw this run of the container; the
	// window counts from it, so that a panel started later still finds
	// the version of a container that runs for long.
	seen    time.Time
	since   time.Time // the next attempt reads the log from here
	next    time.Time // the next attempt is due
	version string
}

// volumeState holds the sizes of an agent's volumes.
type volumeState struct {
	sizes map[string]int64
	at    time.Time // measured
	next  time.Time // next measurement due
}

// insight is the version search and the volume sizes of the view.
type insight struct {
	now  func() time.Time
	kick chan struct{}

	mu       sync.Mutex
	versions map[string]*versionState // by server ID
	volumes  map[string]*volumeState  // by agent ID
	stale    time.Duration
}

func newInsight() *insight {
	return &insight{now: time.Now, kick: make(chan struct{}, 1), versions: map[string]*versionState{},
		volumes: map[string]*volumeState{}, stale: DefaultVolumeStale}
}

// Wake asks for a pass soon, for example after a server got another
// container (AGT-006, AGT-007).
func (v *View) Wake() { v.in.wake() }

// wake asks for a pass soon, without waiting.
func (in *insight) wake() {
	select {
	case in.kick <- struct{}{}:
	default:
	}
}

// Run searches versions and measures volumes until ctx ends: after a
// change of a container or a connected agent at once, else every
// agent.version_retry.
func (v *View) Run(ctx context.Context) {
	for {
		v.Pass(ctx)
		t := time.NewTimer(v.duration(ctx, SettingVersionRetry, DefaultVersionRetry, MinVersionRetry))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-v.in.kick:
		case <-t.C:
		}
		t.Stop()
	}
}

// duration reads a duration setting; a missing or too small value takes
// the default or the minimum.
func (v *View) duration(ctx context.Context, key string, def, low time.Duration) time.Duration {
	var s string
	if err := v.st.GlobalSetting(ctx, key, &s); err != nil {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return max(d, low)
}

// capable reports whether the agent reported the capability.
func (v *View) capable(agentID, capability string) bool {
	caps, _ := v.agents.Capabilities(agentID)
	return slices.Contains(caps, capability)
}

// Pass runs one round: due version attempts and volume measurements. An
// agent without the capability is not asked (AGT-013).
func (v *View) Pass(ctx context.Context) {
	retry := v.duration(ctx, SettingVersionRetry, DefaultVersionRetry, MinVersionRetry)
	window := v.duration(ctx, SettingVersionWindow, DefaultVersionWindow, MinVersionWindow)
	interval := v.duration(ctx, SettingVolumeInterval, DefaultVolumeInterval, MinVolumeInterval)
	stale := v.duration(ctx, SettingVolumeStale, DefaultVolumeStale, MinVolumeStale)
	v.in.mu.Lock()
	v.in.stale = stale
	v.in.mu.Unlock()
	rows, err := v.st.Queries.ListServers(ctx)
	if err != nil {
		v.log.Error("servers not listed for the agent data", slog.Any("err", err))
		return
	}
	seen := map[string]bool{}
	agents := map[string][]string{} // agent → its servers
	for _, r := range rows {
		if !r.AgentID.Valid || !r.ContainerName.Valid {
			continue
		}
		seen[r.ID] = true
		agents[r.AgentID.String] = append(agents[r.AgentID.String], r.ID)
		pat := ""
		if r.ProfileID.Valid {
			pat = v.versionPattern(ctx, r.ProfileID.String)
		}
		v.versionAttempt(ctx, r.ID, r.AgentID.String, r.ContainerName.String, pat, retry, window)
	}
	v.in.mu.Lock()
	for id := range v.in.versions {
		if !seen[id] {
			delete(v.in.versions, id)
		}
	}
	v.in.mu.Unlock()
	for agentID, servers := range agents {
		v.measure(ctx, agentID, servers, retry, interval)
	}
}

func (v *View) versionPattern(ctx context.Context, profileID string) string {
	gp, err := v.st.Queries.GetGameProfile(ctx, profileID)
	if err != nil {
		return ""
	}
	var p profiles.Profile
	if err := json.Unmarshal([]byte(gp.Rules), &p); err != nil || p.Version == nil {
		return ""
	}
	return p.Version.Pattern
}

// versionAttempt asks the agent for the game version of a running
// container once it is due: from the container start on, then every retry
// only the log since the last attempt, until a match or the end of the
// window after the panel first saw this run (AGT-006). A new start, image
// or pattern starts over.
func (v *View) versionAttempt(ctx context.Context, serverID, agentID, container, pat string, retry, window time.Duration) {
	c, ok := v.data.Container(agentID, container)
	if !ok || c.State != "running" || c.StartedAt.IsZero() || pat == "" {
		return
	}
	now := v.in.now()
	key := c.ID + "|" + c.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + c.Image + "|" + pat
	v.in.mu.Lock()
	st := v.in.versions[serverID]
	if st == nil || st.key != key {
		cleared := st != nil && st.version != ""
		st = &versionState{key: key, seen: now, since: c.StartedAt, next: now}
		v.in.versions[serverID] = st
		if cleared {
			defer v.publishRuntime(ctx, serverID)
		}
	}
	due := st.version == "" && !now.Before(st.next) && !now.After(st.seen.Add(window))
	since := st.since
	v.in.mu.Unlock()
	if !due || !v.capable(agentID, agentproto.CapVersionDetect) {
		return
	}
	var found agentproto.VersionFound
	collect := func(e agentproto.Envelope) {
		if e.Type != agentproto.TypeVersionFound {
			return
		}
		if f, err := agentproto.Payload[agentproto.VersionFound](e); err == nil {
			f.Clean()
			found = f
		}
	}
	octx, cancel := context.WithTimeout(ctx, orderTimeout)
	res, err := v.agents.RequestData(octx, agentID, agentproto.TypeVersionDetect,
		agentproto.VersionDetect{Container: container, Pattern: pat, Since: since}, collect)
	cancel()
	v.in.mu.Lock()
	if v.in.versions[serverID] != st {
		v.in.mu.Unlock()
		return
	}
	st.next = now.Add(retry)
	ok = err == nil && res.Code == agentproto.CodeOK
	if ok {
		st.since = now
		st.version = found.Version
	}
	v.in.mu.Unlock()
	if !ok {
		v.log.Info("version search not carried out", slog.String("agent_id", agentID), slog.String("container", container),
			slog.String("code", res.Code), slog.Any("err", err))
		return
	}
	if found.Version != "" {
		v.publishRuntime(ctx, serverID)
	}
}

// measure asks an agent for the sizes of its volumes once per interval,
// one order for all its containers (AGT-007).
func (v *View) measure(ctx context.Context, agentID string, servers []string, retry, interval time.Duration) {
	now := v.in.now()
	v.in.mu.Lock()
	vs := v.in.volumes[agentID]
	due := vs == nil || !now.Before(vs.next)
	v.in.mu.Unlock()
	if !due || !v.data.Connected(agentID) || !v.capable(agentID, agentproto.CapVolumesSize) {
		return
	}
	var got *agentproto.VolumeSizes
	collect := func(e agentproto.Envelope) {
		if e.Type != agentproto.TypeVolumeSizes {
			return
		}
		if s, err := agentproto.Payload[agentproto.VolumeSizes](e); err == nil {
			s.Clean()
			got = &s
		}
	}
	octx, cancel := context.WithTimeout(ctx, orderTimeout)
	res, err := v.agents.RequestData(octx, agentID, agentproto.TypeVolumesSize, agentproto.VolumesSize{}, collect)
	cancel()
	v.in.mu.Lock()
	if vs == nil {
		vs = &volumeState{}
		v.in.volumes[agentID] = vs
	}
	ok := err == nil && res.Code == agentproto.CodeOK && got != nil
	if !ok {
		vs.next = now.Add(retry)
		v.in.mu.Unlock()
		v.log.Info("volume sizes not read", slog.String("agent_id", agentID), slog.String("code", res.Code), slog.Any("err", err))
		return
	}
	vs.sizes, vs.at, vs.next = map[string]int64{}, now, now.Add(interval)
	for _, s := range got.Volumes {
		vs.sizes[s.Name] = s.Bytes
	}
	v.in.mu.Unlock()
	for _, id := range servers {
		v.publishRuntime(ctx, id)
	}
}

// gameVersion returns the found game version of a server.
func (v *View) gameVersion(serverID string) string {
	v.in.mu.Lock()
	defer v.in.mu.Unlock()
	if st := v.in.versions[serverID]; st != nil {
		return st.version
	}
	return ""
}

// VolumeView is a volume of a container with its measured size.
type VolumeView struct {
	Name       string     `json:"name"`
	SizeBytes  *int64     `json:"size_bytes,omitempty"`
	MeasuredAt *time.Time `json:"measured_at,omitempty"`
	Stale      bool       `json:"stale,omitempty"`
}

// volumesOf lists the volumes of a container with the sizes the agent
// measured; without a measurement only the names (AGT-007).
func (v *View) volumesOf(agentID string, names []string) []VolumeView {
	v.in.mu.Lock()
	defer v.in.mu.Unlock()
	vs := v.in.volumes[agentID]
	out := make([]VolumeView, 0, len(names))
	for _, n := range names {
		vv := VolumeView{Name: n}
		if vs != nil {
			if b, ok := vs.sizes[n]; ok && b >= 0 {
				at := vs.at
				vv.SizeBytes, vv.MeasuredAt = &b, &at
				vv.Stale = v.in.now().Sub(vs.at) > v.in.stale
			}
		}
		out = append(out, vv)
	}
	return out
}
