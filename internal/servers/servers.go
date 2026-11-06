// SPDX-License-Identifier: AGPL-3.0-or-later

// Package servers administers game servers and server groups (SRV-001,
// SRV-003, SRV-005). Only the RCON admin changes them; everyone else sees
// the servers of his scope, and servers outside it look like not existing
// (Design 3.1). RCON credentials are encrypted (SRV-002) and never leave
// the panel through the API, the log or the audit.
package servers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Limits of server fields.
const (
	maxNameLen     = 64
	maxHostLen     = 253
	maxPasswordLen = 512
)

// Credential states of a server.
const (
	CredentialsOK      = "ok"
	CredentialsMissing = "missing"
	// CredentialsReenter means the credentials were sealed with another
	// master key and must be entered again (SRV-002).
	CredentialsReenter = "reenter"
)

// Server is a game server. Connection fields are filled only for callers
// who may see them (RCON admin and server admin).
type Server struct {
	ID   string
	Name string
	// Status is the reachability (SRV-004); everyone in scope sees it.
	Status      connmgr.Status
	StatusSince time.Time
	ProfileID   string
	GroupIDs    []string
	Detailed    bool
	Protocol    string
	Host        string
	Port        int
	Credentials string
	AgentID     string
	// ContainerName is the container on the agent's host; the agent
	// resolves it and checks the label each time (revision 2.3 cand. 1).
	ContainerName string
	// GamePorts are the effective game ports: the override of the server,
	// or else those of the profile (SRV-005).
	GamePorts         []profiles.GamePort
	GamePortsOverride bool
}

// Input is the writable part of a server; nil fields keep their value on
// update. Password is write-only.
type Input struct {
	Name      *string
	ProfileID *string
	Host      *string
	Port      *int
	Password  *logging.Secret
	// GamePorts overrides the profile's game ports; an empty slice
	// returns to the profile's.
	GamePorts *[]profiles.GamePort
	// AgentID and ContainerName tie the server to a container of a paired
	// agent; "" clears them (SRV-001 rev. 2).
	AgentID       *string
	ContainerName *string
	// Version is the version read before (required); the write is refused
	// with ErrConflict if it is stale.
	Version *int64
}

// Service administers servers and server groups.
type Service struct {
	st    *store.Store
	perm  *perm.Checker
	box   *secret.Box
	log   *slog.Logger
	conns Connections
	// agents and rcon carry out actions (AGT-003, AGT-009).
	agents Agents
	rcon   Executor
}

// NewService returns the server administration; box encrypts credentials
// with the current master key.
func NewService(st *store.Store, c *perm.Checker, box *secret.Box, log *slog.Logger) *Service {
	return &Service{st: st, perm: c, box: box, log: log}
}

func secretContext(id string) secret.Context {
	return secret.Context{Table: "servers", ID: id, Column: "secret"}
}

// credentialState tells whether the stored credentials can be used.
func (s *Service) credentialState(value []byte) string {
	if len(value) == 0 {
		return CredentialsMissing
	}
	if id, err := secret.KeyIDOf(value); err != nil || id != s.box.KeyID() {
		return CredentialsReenter
	}
	return CredentialsOK
}

// CheckCredentials logs at start how many servers need their credentials
// entered again, for example after the master key was lost (SRV-002).
func (s *Service) CheckCredentials(ctx context.Context) error {
	rows, err := s.st.Queries.ListServers(ctx)
	if err != nil {
		return err
	}
	n := 0
	for _, r := range rows {
		if s.credentialState(r.Secret) == CredentialsReenter {
			n++
		}
	}
	if n > 0 {
		s.log.Warn("RCON credentials were encrypted with another master key; enter them again",
			slog.Int("servers", n))
	}
	return nil
}

// Credentials returns the decrypted credentials of a server for the
// connection manager; they never go to API, log or audit.
func (s *Service) Credentials(ctx context.Context, id string) (logging.Secret, error) {
	r, err := s.st.Queries.GetServer(ctx, id)
	if err != nil {
		return "", err
	}
	return s.box.Open(r.Secret, secretContext(id))
}

func (s *Service) denied(ctx context.Context, req users.Request, action, targetType, target, reason string) {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Server: target, Reason: reason, Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: targetType, TargetID: target, Action: action, Result: audit.ResultDenied, Reason: reason,
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
}

func (s *Service) auditOK(ctx context.Context, tx *sql.Tx, req users.Request, action, targetType, target, server string, payload any) error {
	_, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		ServerID: server, TargetType: targetType, TargetID: target, Action: action, Payload: payload, Result: audit.ResultOK,
	})
	return err
}

// mustBeAdmin allows changes only to the RCON admin (Design 2.2).
func (s *Service) mustBeAdmin(ctx context.Context, req users.Request, action, targetType, target string) error {
	if req.Actor.Role == perm.RCONAdmin {
		return nil
	}
	s.denied(ctx, req, action, targetType, target, "forbidden")
	return users.ErrForbidden
}

func (s *Service) memberships(ctx context.Context) (map[string][]string, error) {
	rows, err := s.st.Queries.ListServerGroupMemberships(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		out[r.ServerID] = append(out[r.ServerID], r.ServerGroupID)
	}
	return out, nil
}

func (s *Service) toServer(ctx context.Context, r sqlitedb.ListServersRow, groups []string, detailed bool) (Server, error) {
	srv := Server{ID: r.ID, Name: r.Name, ProfileID: r.ProfileID.String, GroupIDs: groups, Detailed: detailed, Status: connmgr.Offline}
	if s.conns != nil {
		srv.Status, srv.StatusSince = s.conns.Status(r.ID)
	}
	if srv.GroupIDs == nil {
		srv.GroupIDs = []string{}
	}
	if !detailed {
		return srv, nil
	}
	srv.Protocol, srv.Host, srv.Port = r.Protocol, r.Host, int(r.Port)
	srv.Credentials = s.credentialState(r.Secret)
	srv.AgentID, srv.ContainerName = r.AgentID.String, r.ContainerName.String
	srv.GamePorts = []profiles.GamePort{}
	if r.GamePorts.Valid {
		srv.GamePortsOverride = true
		if err := json.Unmarshal([]byte(r.GamePorts.String), &srv.GamePorts); err != nil {
			return Server{}, err
		}
	} else if r.ProfileID.Valid {
		p, err := s.st.Queries.GetGameProfile(ctx, r.ProfileID.String)
		if err == nil {
			var rules profiles.Profile
			if json.Unmarshal([]byte(p.Rules), &rules) == nil && rules.Ports.Game != nil {
				srv.GamePorts = rules.Ports.Game
			}
		}
	}
	return srv, nil
}

func detailedFor(r perm.Role) bool { return r == perm.RCONAdmin || r == perm.ServerAdmin }

// List returns the servers in the caller's scope (ROLE-003).
func (s *Service) List(ctx context.Context, req users.Request) ([]Server, error) {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	groups, err := s.memberships(ctx)
	if err != nil {
		return nil, err
	}
	out := []Server{}
	for _, r := range rows {
		if _, ok := rights.Server(r.ID); !ok {
			continue
		}
		srv, err := s.toServer(ctx, r, s.visibleGroups(ctx, req, groups[r.ID]), detailedFor(rights.Role))
		if err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	return out, nil
}

// visibleGroups hides groups that are not fully in the caller's scope
// from everyone but the RCON admin.
func (s *Service) visibleGroups(ctx context.Context, req users.Request, ids []string) []string {
	if req.Actor.Role == perm.RCONAdmin {
		return ids
	}
	var out []string
	for _, g := range ids {
		if ok, _ := s.groupInScope(ctx, req, g); ok {
			out = append(out, g)
		}
	}
	return out
}

// Get returns a server in the caller's scope; any other server answers
// ErrNotFound, logged as perm.denied (ROLE-003, Design 3.1).
func (s *Service) Get(ctx context.Context, req users.Request, id string) (Server, error) {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return Server{}, err
	}
	if _, ok := rights.Server(id); !ok {
		s.denied(ctx, req, "server.read", "server", id, "not_found")
		return Server{}, users.ErrNotFound
	}
	r, err := s.st.Queries.GetServer(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, users.ErrNotFound
	}
	if err != nil {
		return Server{}, err
	}
	groups, err := s.memberships(ctx)
	if err != nil {
		return Server{}, err
	}
	return s.toServer(ctx, sqlitedb.ListServersRow(r), s.visibleGroups(ctx, req, groups[id]), detailedFor(rights.Role))
}

func plainName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || utf8.RuneCountInString(v) > maxNameLen {
		return "", &users.InvalidError{Detail: fmt.Sprintf("a name has 1 to %d characters", maxNameLen)}
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", &users.InvalidError{Detail: "a name must not contain control characters"}
		}
	}
	return v, nil
}

func validHost(h string) error {
	if h == "" || len(h) > maxHostLen {
		return &users.InvalidError{Detail: fmt.Sprintf("host has 1 to %d characters", maxHostLen)}
	}
	if net.ParseIP(h) != nil {
		return nil
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return &users.InvalidError{Detail: "host must be an IP address or a host name"}
		}
		for _, r := range label {
			if !hostChar(r) {
				return &users.InvalidError{Detail: "host must be an IP address or a host name"}
			}
		}
	}
	return nil
}

func hostChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

func validPorts(ports []profiles.GamePort) error {
	for _, p := range ports {
		if p.Port < 1 || p.Port > 65535 || (p.Proto != "tcp" && p.Proto != "udp") || strings.TrimSpace(p.Name) == "" || len(p.Name) > maxNameLen {
			return &users.InvalidError{Detail: "a game port has port 1 to 65535, proto tcp or udp and a name"}
		}
	}
	return nil
}

// fields is the checked state of a server before writing.
type fields struct {
	name, profileID, protocol, host string
	port                            int
	gamePorts                       sql.NullString
	agentID, container              sql.NullString
}

func (s *Service) check(ctx context.Context, f fields, in Input) (fields, error) {
	var err error
	if in.Name != nil {
		if f.name, err = plainName(*in.Name); err != nil {
			return f, err
		}
	}
	if in.ProfileID != nil {
		p, err := s.st.Queries.GetGameProfile(ctx, *in.ProfileID)
		if errors.Is(err, sql.ErrNoRows) {
			return f, &users.InvalidError{Detail: "unknown profile"}
		}
		if err != nil {
			return f, err
		}
		if f.profileID != p.ID && in.Port == nil {
			f.port = int(p.DefaultPort)
		}
		f.profileID, f.protocol = p.ID, p.Protocol
	}
	if in.Host != nil {
		h := strings.TrimSpace(*in.Host)
		if err := validHost(h); err != nil {
			return f, err
		}
		f.host = h
	}
	if in.Port != nil {
		if *in.Port < 1 || *in.Port > 65535 {
			return f, &users.InvalidError{Detail: "port must be 1 to 65535"}
		}
		f.port = *in.Port
	}
	if in.GamePorts != nil {
		if len(*in.GamePorts) == 0 {
			f.gamePorts = sql.NullString{}
		} else {
			if err := validPorts(*in.GamePorts); err != nil {
				return f, err
			}
			b, _ := json.Marshal(*in.GamePorts)
			f.gamePorts = sql.NullString{String: string(b), Valid: true}
		}
	}
	if in.AgentID != nil {
		id := strings.TrimSpace(*in.AgentID)
		f.agentID = sql.NullString{String: id, Valid: id != ""}
		if id == "" {
			f.container = sql.NullString{}
		} else {
			a, err := s.st.Queries.GetAgent(ctx, id)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && a.Status != "active") {
				return f, &users.InvalidError{Detail: "unknown or inactive agent"}
			}
			if err != nil {
				return f, err
			}
		}
	}
	if in.ContainerName != nil {
		n := strings.TrimSpace(*in.ContainerName)
		if n != "" && !validContainerName(n) {
			return f, &users.InvalidError{Detail: "a container name has letters, digits, _ . - (at most 255)"}
		}
		f.container = sql.NullString{String: n, Valid: n != ""}
	}
	if f.container.Valid != f.agentID.Valid {
		return f, &users.InvalidError{Detail: "agent_id and container_name go together"}
	}
	if !slices.Contains(profiles.Protocols, f.protocol) {
		return f, &users.InvalidError{Detail: "the profile's protocol is not supported"}
	}
	return f, nil
}

// validContainerName follows Docker's container names.
func validContainerName(n string) bool {
	if len(n) > 255 {
		return false
	}
	for i, r := range n {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || (i > 0 && (r == '_' || r == '.' || r == '-'))
		if !ok {
			return false
		}
	}
	return n != ""
}

func checkPassword(pw logging.Secret) error {
	if n := utf8.RuneCountInString(pw.Reveal()); n < 1 || n > maxPasswordLen {
		return &users.InvalidError{Detail: fmt.Sprintf("the password has 1 to %d characters", maxPasswordLen)}
	}
	return nil
}

// changedFields names what an input changes, for the audit; the password
// appears only as "password", never its value.
func changedFields(in Input) []string {
	var out []string
	for name, set := range map[string]bool{
		"name": in.Name != nil, "profile_id": in.ProfileID != nil, "host": in.Host != nil,
		"port": in.Port != nil, "password": in.Password != nil, "game_ports": in.GamePorts != nil,
		"agent_id": in.AgentID != nil, "container_name": in.ContainerName != nil,
	} {
		if set {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Create adds a server; name, profile, host and password are required, the
// port defaults to the profile's (SRV-001).
func (s *Service) Create(ctx context.Context, req users.Request, in Input) (Server, error) {
	if err := s.mustBeAdmin(ctx, req, "server.create", "server", ""); err != nil {
		return Server{}, err
	}
	if in.Name == nil || in.ProfileID == nil || in.Host == nil || in.Password == nil {
		return Server{}, &users.InvalidError{Detail: "name, profile_id, host and password are required"}
	}
	f, err := s.check(ctx, fields{}, in)
	if err != nil {
		return Server{}, err
	}
	if err := checkPassword(*in.Password); err != nil {
		return Server{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return Server{}, err
	}
	sealed, err := s.box.Seal(*in.Password, secretContext(id))
	if err != nil {
		return Server{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	if err := sqlitedb.New(tx).InsertServer(ctx, sqlitedb.InsertServerParams{
		ID: id, Name: f.name, ProfileID: sql.NullString{String: f.profileID, Valid: true}, Protocol: f.protocol,
		Host: f.host, Port: int64(f.port), Secret: sealed, GamePorts: f.gamePorts, AgentID: f.agentID, ContainerName: f.container,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Server{}, &users.InvalidError{Detail: "a server with this name exists"}
		}
		return Server{}, err
	}
	if err := s.auditOK(ctx, tx, req, "server.create", "server", id, id, map[string]any{"name": f.name, "fields": changedFields(in)}); err != nil {
		return Server{}, err
	}
	if err := tx.Commit(); err != nil {
		return Server{}, err
	}
	s.perm.Invalidate()
	s.changed(ctx, id)
	return s.Get(ctx, req, id)
}

// Update changes a server; a new password replaces the old one.
func (s *Service) Update(ctx context.Context, req users.Request, id string, in Input) (Server, error) {
	if err := s.mustBeAdmin(ctx, req, "server.update", "server", id); err != nil {
		return Server{}, err
	}
	r, err := s.st.Queries.GetServer(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, users.ErrNotFound
	}
	if err != nil {
		return Server{}, err
	}
	f, err := s.check(ctx, fields{
		name: r.Name, profileID: r.ProfileID.String, protocol: r.Protocol, host: r.Host, port: int(r.Port), gamePorts: r.GamePorts,
		agentID: r.AgentID, container: r.ContainerName,
	}, in)
	if err != nil {
		return Server{}, err
	}
	// The stored password must not travel to a new address unasked: a stolen
	// session could read it there (T-011).
	if in.Password == nil && (f.host != r.Host || f.port != int(r.Port)) {
		return Server{}, &users.InvalidError{Detail: "enter the RCON password again when host or port change"}
	}
	var sealed []byte
	if in.Password != nil {
		if err := checkPassword(*in.Password); err != nil {
			return Server{}, err
		}
		if sealed, err = s.box.Seal(*in.Password, secretContext(id)); err != nil {
			return Server{}, err
		}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	// Optimistic locking in one statement (revision 2.4 candidate 24);
	// the version is required, checked after role and scope.
	if in.Version == nil {
		return Server{}, users.ErrPreconditionRequired
	}
	n, err := q.BumpServerVersion(ctx, sqlitedb.BumpServerVersionParams{ID: id, Version: *in.Version})
	if err != nil {
		return Server{}, err
	}
	if n == 0 {
		return Server{}, users.ErrConflict
	}
	now := store.Now()
	if err := q.UpdateServer(ctx, sqlitedb.UpdateServerParams{
		Name: f.name, ProfileID: sql.NullString{String: f.profileID, Valid: f.profileID != ""}, Protocol: f.protocol,
		Host: f.host, Port: int64(f.port), GamePorts: f.gamePorts, AgentID: f.agentID, ContainerName: f.container, UpdatedAt: now, ID: id,
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Server{}, &users.InvalidError{Detail: "a server with this name exists"}
		}
		return Server{}, err
	}
	if sealed != nil {
		if err := q.SetServerSecret(ctx, sqlitedb.SetServerSecretParams{Secret: sealed, UpdatedAt: now, ID: id}); err != nil {
			return Server{}, err
		}
	}
	if err := s.auditOK(ctx, tx, req, "server.update", "server", id, id, map[string]any{"fields": changedFields(in)}); err != nil {
		return Server{}, err
	}
	if err := tx.Commit(); err != nil {
		return Server{}, err
	}
	s.changed(ctx, id)
	return s.Get(ctx, req, id)
}

// Delete soft-deletes a server (D-028); nobody reaches it any more.
func (s *Service) Delete(ctx context.Context, req users.Request, id string) error {
	if err := s.mustBeAdmin(ctx, req, "server.delete", "server", id); err != nil {
		return err
	}
	r, err := s.st.Queries.GetServer(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return users.ErrNotFound
	}
	if err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	if err := sqlitedb.New(tx).SoftDeleteServer(ctx, sqlitedb.SoftDeleteServerParams{DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	if err := s.auditOK(ctx, tx, req, "server.delete", "server", id, id, map[string]string{"name": r.Name}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.perm.Invalidate()
	s.changed(ctx, id)
	return nil
}
