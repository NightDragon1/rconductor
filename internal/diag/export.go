// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

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
// again. Within diagnostics.max_bytes the agent logs take their part
// first, the system log the rest; the oldest lines of each stay out and
// the file says so. The heads of the files are written also without room:
// about 200 bytes per agent and those of the system log past the budget. Audited with period, size and
// whether anything was cut.
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
	// rw blacks out one encoded line or document: domain values by their
	// key first, then every text value (D-090).
	rw := func(line string) string { return syslog.Rewrite(p.domains(line), clean) }
	type out struct{ name, text string }
	var all []out
	size := 0
	for _, f := range files {
		doc, err := json.Marshal(f.doc)
		if err != nil {
			return nil, err
		}
		all = append(all, out{f.name, rw(string(doc)) + "\n"})
	}
	for _, o := range all {
		size += len(o.name) + len(o.text)
	}
	budget := s.maxBytes(ctx) - size
	agents, err := s.st.Queries.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	var active []string
	for _, a := range agents {
		if a.Status == "active" {
			active = append(active, a.ID)
		}
	}
	// All agents at once under one deadline, so the slot is not held for
	// one timeout per agent (D-091).
	tails := make([]tailResult, len(active))
	tctx, cancel := context.WithTimeout(ctx, agentTimeout)
	var wg sync.WaitGroup
	for i, id := range active {
		wg.Go(func() {
			r := &tails[i]
			r.lines, r.cut, r.err = s.tail(tctx, id, agentproto.MaxLogLines)
		})
	}
	wg.Wait()
	cancel()
	agentsCut := false
	for i, id := range active {
		name := "agents/" + id + ".log"
		text, used, cut := agentLog(tails[i], rw, budget-len(name))
		agentsCut = agentsCut || cut
		budget -= len(name) + used
		all = append(all, out{name, text})
	}
	text, shortened, err := s.systemLog(ctx, from, to, budget, rw)
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
		Payload: map[string]any{"from": from.UTC(), "to": to.UTC(), "bytes": b.Len(), "shortened": shortened || agentsCut},
	}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type tailResult struct {
	lines []string
	cut   bool
	err   error
}

// agentLog writes the log of one agent: the head, then its newest lines as
// far as budget bytes allow; used counts the text beyond the head
// (diagnostics.max_bytes, D-091).
func agentLog(t tailResult, rw func(string) string, budget int) (text string, used int, cut bool) {
	head := Notice + "\n"
	switch {
	case errors.Is(t.err, ErrAgentUnavailable):
		head += "The agent is offline; its log is not in this package.\n"
	case errors.Is(t.err, ErrAgentUnsupported):
		head += "The agent does not send its own log.\n"
	case t.err != nil:
		head += "The agent could not read its log.\n"
	case t.cut:
		head += "Only the last lines of the log.\n"
	}
	budget -= len(head)
	var lines []string
	for i := len(t.lines) - 1; i >= 0; i-- {
		l := oneLine(rw(t.lines[i])) + "\n"
		if used+len(l) > budget {
			head, cut = head+"Shortened by diagnostics.max_bytes.\n", true
			break
		}
		lines, used = append(lines, l), used+len(l)
	}
	slices.Reverse(lines)
	return head + strings.Join(lines, ""), len(head) + used, cut
}

// systemLog writes the stored lines of the period, oldest first, through
// rw; beyond budget bytes the oldest stay out, and the head says from
// when the lines are.
func (s *Service) systemLog(ctx context.Context, from, to time.Time, budget int, rw func(string) string) (string, bool, error) {
	const page = 1000
	var lines, stamps []string
	size, shortened := 0, budget <= 0
	// Without room no line is read; the head says so (D-091).
	for after := int64(0); budget > 0; {
		rows, err := s.st.Queries.ListSystemLog(ctx, sqlitedb.ListSystemLogParams{
			FromTs: from.UTC().Format(store.TimeLayout), ToTs: to.UTC().Format(store.TimeLayout), After: after, MaxRows: page,
		})
		if err != nil {
			return "", false, err
		}
		for _, r := range rows {
			l := rw(r.Text) + "\n"
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
	// The mail server is a host the panel stores too (D-090).
	if m, err := q.GetSMTPSettings(ctx); err == nil {
		known = append(known, m.Host)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
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

// oneLine keeps a foreign log line on its line and in its order: control
// characters other than the tab (line breaks included), line and paragraph
// separators and format characters (bidi controls) become U+FFFD (SEC-006).
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\t' && (unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf)) {
			return '\uFFFD'
		}
		return r
	}, s)
}
