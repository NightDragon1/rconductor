// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/syslog"
	"github.com/NightDragon1/rconductor/internal/users"
)

// redacted stands for a value a package never holds (SEC-012).
const redacted = "[redacted]"

// Export builds the diagnostic package (NFR-018, D-090): role first (403,
// audited), then the period (400). One JSON file per area in a fixed
// schema, never a raw dump; every file goes through syslog.Rewrite, so its
// values are masked (D-088), blacked out and pseudonymized as they were
// stored, before the file is encoded again. Audited with period and size.
func (s *Service) Export(ctx context.Context, req users.Request, from, to time.Time) ([]byte, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return nil, s.denied(ctx, req, "diagnostics.exported")
	}
	if err := period(from, to); err != nil {
		return nil, err
	}
	files, known, err := s.areas(ctx)
	if err != nil {
		return nil, err
	}
	p := newPseudonyms(known)
	clean := func(texts []string) []string {
		if s.mask != nil {
			texts, _ = s.mask.Lines(texts, nil)
		}
		return p.apply(texts)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range files {
		doc, err := json.Marshal(f.doc)
		if err != nil {
			return nil, err
		}
		w, err := z.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(syslog.Rewrite(string(doc), clean) + "\n")); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "diagnostics", Action: "diagnostics.exported", Result: audit.ResultOK,
		Payload: map[string]any{"from": from.UTC(), "to": to.UTC(), "bytes": b.Len()},
	}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
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
