// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Connections is what the server administration needs from the connection
// manager.
type Connections interface {
	Reload(ctx context.Context, id string) error
	Status(id string) (connmgr.Status, time.Time)
	Try(ctx context.Context, protocol, addr string, password logging.Secret) error
}

// SetConnections links the connection manager: it is told about every
// change and reports the reachability shown with each server (SRV-004).
func (s *Service) SetConnections(c Connections) { s.conns = c }

func (s *Service) changed(ctx context.Context, id string) {
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
		Addr: net.JoinHostPort(r.Host, strconv.Itoa(int(r.Port))),
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
