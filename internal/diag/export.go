// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/syslog"
	"github.com/NightDragon1/rconductor/internal/users"
)

// redacted stands for a value a package never holds (SEC-012).
const redacted = "[redacted]"

// Notice heads every log file of a package (D-091, UI-025).
const Notice = "May contain chat messages of players."

// ErrBusy means diagnostics.parallel packages are being built (503).
var ErrBusy = errors.New("too many diagnostic packages at the same time")

// Export builds the diagnostic package (NFR-018, D-090, D-091): role first
// (403, audited), then the period (400), then a slot of
// diagnostics.parallel (ErrBusy). One JSON file per area in a fixed schema,
// never a raw dump, the log of every active agent (or why it is missing)
// and the system log of the period as text. Every file goes through
// syslog.Rewrite, so its values are masked (D-088), blacked out and
// pseudonymized as they were stored or logged, before the file is encoded
// again. Above diagnostics.max_bytes the oldest lines of the system log
// stay out and the package says so. Audited with period and size.
func (s *Service) Export(ctx context.Context, req users.Request, from, to time.Time) ([]byte, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return nil, s.denied(ctx, req, "diagnostics.exported")
	}
	if err := period(from, to); err != nil {
		return nil, err
	}
	release, ok := s.acquire(ctx)
	if !ok {
		return nil, ErrBusy
	}
	defer release()
	files, known, err := s.areas(ctx)
	if err != nil {
		return nil, err
	}
	p := newPseudonyms(known)
	// Without a masker the built-in list still applies (D-107, fail closed).
	clean := func(texts []string) []string {
		if s.mask != nil {
			texts, _ = s.mask.Lines(texts, nil)
		} else {
			texts = logmask.ListLines(texts)
		}
		return p.apply(texts)
	}
	type out struct{ name, text string }
	var all []out
	size := 0
	for _, f := range files {
		doc, err := json.Marshal(f.doc)
		if err != nil {
			return nil, err
		}
		all = append(all, out{f.name, syslog.Rewrite(string(doc), clean) + "\n"})
	}
	agents, err := s.st.Queries.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if a.Status != "active" {
			continue
		}
		var b strings.Builder
		b.WriteString(Notice + "\n")
		lines, cut, err := s.tail(ctx, a.ID, agentproto.MaxLogLines)
		switch {
		case errors.Is(err, ErrAgentUnavailable):
			b.WriteString("The agent is offline; its log is not in this package.\n")
		case errors.Is(err, ErrAgentUnsupported):
			b.WriteString("The agent does not send its own log.\n")
		case err != nil:
			b.WriteString("The agent could not read its log.\n")
		case cut:
			b.WriteString("Only the last lines of the log.\n")
		}
		for _, l := range lines {
			b.WriteString(oneLine(syslog.Rewrite(l, clean)) + "\n")
		}
		all = append(all, out{"agents/" + a.ID + ".log", b.String()})
	}
	for _, o := range all {
		size += len(o.name) + len(o.text)
	}
	text, shortened, err := s.systemLog(ctx, from, to, s.maxBytes(ctx)-size, clean)
	if err != nil {
		return nil, err
	}
	all = append(all, out{"system-log.txt", text})
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, o := range all {
		w, err := z.Create(o.name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, o.text); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "diagnostics", Action: "diagnostics.exported", Result: audit.ResultOK,
		Payload: map[string]any{"from": from.UTC(), "to": to.UTC(), "bytes": b.Len(), "shortened": shortened},
	}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// systemLog writes the stored lines of the period, oldest first, through
// clean; beyond budget bytes the oldest stay out, and the head says from
// when the lines are.
func (s *Service) systemLog(ctx context.Context, from, to time.Time, budget int, clean func([]string) []string) (string, bool, error) {
	const page = 1000
	var lines, stamps []string
	size, shortened := 0, false
	for after := int64(0); ; {
		rows, err := s.st.Queries.ListSystemLog(ctx, sqlitedb.ListSystemLogParams{
			FromTs: from.UTC().Format(store.TimeLayout), ToTs: to.UTC().Format(store.TimeLayout), After: after, MaxRows: page,
		})
		if err != nil {
			return "", false, err
		}
		for _, r := range rows {
			l := syslog.Rewrite(r.Text, clean) + "\n"
			lines, stamps, size, after = append(lines, l), append(stamps, r.Ts), size+len(l), r.ID
			for size > budget && len(lines) > 0 {
				size, lines, stamps, shortened = size-len(lines[0]), lines[1:], stamps[1:], true
			}
		}
		if len(rows) < page {
			break
		}
	}
	head := Notice + "\nPeriod " + from.UTC().Format(time.RFC3339) + " to " + to.UTC().Format(time.RFC3339) + ".\n"
	if shortened {
		since := to.UTC().Format(time.RFC3339)
		if len(stamps) > 0 {
			since = stamps[0]
		}
		head += "Shortened by diagnostics.max_bytes: lines from " + since + " only.\n"
	}
	return head + strings.Join(lines, ""), shortened, nil
}

// maxBytes is diagnostics.max_bytes; a value not readable is the last one
// read or the default (F-93).
func (s *Service) maxBytes(ctx context.Context) int {
	n := users.DefaultDiagnosticsMaxBytes
	if err := s.st.GuardedSetting(ctx, s.log, users.SettingDiagnosticsMaxBytes, &n); err != nil || n < users.MinDiagnosticsMaxBytes {
		n = users.DefaultDiagnosticsMaxBytes
	}
	return n
}

// acquire takes one of diagnostics.parallel slots.
func (s *Service) acquire(ctx context.Context) (func(), bool) {
	n := users.DefaultDiagnosticsParallel
	if err := s.st.GuardedSetting(ctx, s.log, users.SettingDiagnosticsParallel, &n); err != nil || n < users.MinDiagnosticsParallel {
		n = users.DefaultDiagnosticsParallel
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running >= n {
		return nil, false
	}
	s.running++
	return func() { s.mu.Lock(); s.running--; s.mu.Unlock() }, true
}

type file struct {
	name string
	doc  any
}

// areas reads the areas of D-090 in their fixed schema; known are the
// hosts the panel stores, for the pseudonyms.
func (s *Service) areas(ctx context.Context) ([]file, []string, error) {
	q := s.st.Queries
	var known []string
	agents, err := q.ListAgents(ctx)
	if err != nil {
		return nil, nil, err
	}
	type agentState struct {
		ID, Name, DisplayName, Status, Version, Remote, LastSeen string
		Capabilities                                             json.RawMessage
	}
	type agentVersion struct{ ID, Version string }
	var states []agentState
	var versions []agentVersion
	for _, a := range agents {
		states = append(states, agentState{ID: a.ID, Name: a.Name, DisplayName: a.DisplayName.String, Status: a.Status,
			Version: a.Version, Remote: a.Remote, LastSeen: a.LastSeenAt.String, Capabilities: rawOr(a.Capabilities, "[]")})
		versions = append(versions, agentVersion{ID: a.ID, Version: a.Version})
		host, _, err := net.SplitHostPort(a.Remote)
		if err != nil {
			host = a.Remote
		}
		known = append(known, a.Name, host)
	}
	servers, err := q.ListServers(ctx)
	if err != nil {
		return nil, nil, err
	}
	type server struct {
		ID, Name, Protocol, Host, AgentID, Container, TLSNotAfter string
		Number, Port                                              int64
	}
	var srv []server
	for _, r := range servers {
		srv = append(srv, server{ID: r.ID, Number: r.Number.Int64, Name: r.Name, Protocol: r.Protocol, Host: r.Host, Port: r.Port,
			AgentID: r.AgentID.String, Container: r.ContainerName.String, TLSNotAfter: r.TlsNotAfter.String})
		known = append(known, r.Host)
	}
	profiles, err := q.ListGameProfiles(ctx)
	if err != nil {
		return nil, nil, err
	}
	type profile struct {
		Slug, Name, Protocol, LastError string
		DefaultPort                     int64
		Overridden                      bool
	}
	var prof []profile
	for _, r := range profiles {
		prof = append(prof, profile{Slug: r.Slug, Name: r.Name, Protocol: r.Protocol, LastError: r.LastError.String,
			DefaultPort: r.DefaultPort, Overridden: r.Overridden != 0})
	}
	rows, err := q.ListGlobalSettings(ctx)
	if err != nil {
		return nil, nil, err
	}
	settings := map[string]json.RawMessage{}
	for _, r := range rows {
		settings[r.Key] = rawOr(r.Value, "null")
		if sensitiveSetting(r.Key) {
			settings[r.Key] = json.RawMessage(`"` + redacted + `"`)
		}
	}
	return []file{
		{"versions.json", map[string]any{"panel": map[string]string{
			"version": buildinfo.Version, "commit": buildinfo.Commit, "build_date": buildinfo.BuildDate}, "agents": versions}},
		{"settings.json", map[string]any{"settings": settings}},
		{"profiles.json", map[string]any{"profiles": prof}},
		{"agents.json", map[string]any{"agents": states}},
		{"servers.json", map[string]any{"servers": srv}},
	}, known, nil
}

// sensitiveSetting is a key whose value never leaves the panel: a secret
// by its name or by its last part, and the texts of the log masking.
func sensitiveSetting(key string) bool {
	last := key[strings.LastIndex(key, ".")+1:]
	return logging.IsSensitiveKey(key) || logging.IsSensitiveKey(last) || key == users.SettingLogMasking
}

// rawOr keeps a stored JSON value as it is, or def when it is not JSON.
func rawOr(v, def string) json.RawMessage {
	if json.Valid([]byte(v)) {
		return json.RawMessage(v)
	}
	return json.RawMessage(def)
}

// oneLine keeps a foreign log line on its line: control characters other
// than the tab, line breaks included, become U+FFFD (SEC-006).
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\t' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
			return '\uFFFD'
		}
		return r
	}, s)
}
