// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Connections is what the server administration needs from the connection
// manager.
type Connections interface {
	Reload(ctx context.Context, id string) error
	Status(id string) (connmgr.Status, time.Time)
	Try(ctx context.Context, t connmgr.Target) error
	Certificate(ctx context.Context, protocol, addr string) (rcon.Certificate, bool, error)
}

// SetConnections links the connection manager: it is told about every
// change and reports the reachability shown with each server (SRV-004).
func (s *Service) SetConnections(c Connections) { s.conns = c }

// OnChange sets a receiver of every changed server, such as the agent
// data that a new container assignment concerns (AGT-006, AGT-007).
func (s *Service) OnChange(f func(id string)) { s.onChange = f }

func (s *Service) changed(ctx context.Context, id string) {
	if s.onChange != nil {
		defer s.onChange(id)
	}
	if s.conns == nil {
		return
	}
	if err := s.conns.Reload(ctx, id); err != nil {
		s.log.Error("connection not reloaded", slog.String("server", id), slog.Any("err", err))
	}
}

func (s *Service) target(r sqlitedb.ListServersRow) connmgr.Target {
	t := connmgr.Target{
		ID: r.ID, Name: r.Name, Protocol: r.Protocol,
		Addr:        net.JoinHostPort(r.Host, strconv.Itoa(int(r.Port))),
		Fingerprint: r.TlsFingerprint,
	}
	if r.Protocol == httpapi.Protocol {
		t.Dialect = s.dialect(r.ProfileID.String)
		t.Parallel = s.httpParallel()
	} else {
		t.EndMarker = s.endMarker(r.ID)
	}
	if s.credentialState(r.Secret) != CredentialsOK {
		return t
	}
	pw, err := s.box.Open(r.Secret, secretContext(r.ID))
	if err != nil {
		s.log.Warn("credentials of a server cannot be opened", slog.String("server", r.ID), slog.Any("err", err))
		return t
	}
	t.Password, t.Usable = pw, true
	return t
}

// Targets returns all servers for the connection manager.
func (s *Service) Targets(ctx context.Context) ([]connmgr.Target, error) {
	rows, err := s.st.Queries.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]connmgr.Target, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.target(r))
	}
	return out, nil
}

// Target returns one server for the connection manager; ok is false for
// deleted or unknown servers.
func (s *Service) Target(ctx context.Context, id string) (connmgr.Target, bool, error) {
	r, err := s.st.Queries.GetServer(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return connmgr.Target{}, false, nil
	}
	if err != nil {
		return connmgr.Target{}, false, err
	}
	return s.target(sqlitedb.ListServersRow(r)), true, nil
}

// dialect is the http-api dialect of a profile (PROT-003, D-071).
func (s *Service) dialect(profileID string) string {
	p, err := s.st.Queries.GetGameProfile(context.Background(), profileID)
	if err != nil {
		return ""
	}
	var rules profiles.Profile
	if json.Unmarshal([]byte(p.Rules), &rules) != nil || rules.HTTPAPI == nil {
		return ""
	}
	return rules.HTTPAPI.Dialect
}

// endMarker is the Source RCON end marker of a server: from the topmost
// profile of its list that sets it, empty for the default (PROT-001,
// D-074).
func (s *Service) endMarker(serverID string) string {
	eff, _, err := profiles.ForServer(context.Background(), s.st.Queries, serverID)
	if err != nil || eff == nil || eff.SourceRCON == nil {
		return ""
	}
	return eff.SourceRCON.EndMarker
}

// httpParallel is servers.http_parallel, the requests at once to one
// server with an HTTPS API.
func (s *Service) httpParallel() int {
	n := users.DefaultHTTPParallel
	if err := s.st.GlobalSetting(context.Background(), users.SettingHTTPParallel, &n); err != nil || n < users.MinHTTPParallel {
		n = users.DefaultHTTPParallel
	}
	return n
}
