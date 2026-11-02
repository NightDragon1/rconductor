// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agents administers the host agents (Design 1.2, 1.7, 2.2):
// pairing by token (way A) or by fingerprint (way B), approval, revocation
// and the admission of connecting agents for the agent hub. Only the RCON
// admin pairs and revokes (AGT-005).
package agents

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Pairing rules (Design 1.2, revision 2.3 candidate 2).
const (
	TokenLifetime  = 24 * time.Hour
	PendingMax     = 10
	PendingTimeout = 24 * time.Hour
)

// Status of an agent.
const (
	StatusPending = "pending"
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// Hub is the part of the agent hub the administration needs.
type Hub interface {
	Fingerprint() string
	Connected(agentID string) bool
	Revoke(agentID string)
}

// Agent is an agent as the API shows it.
type Agent struct {
	ID           string
	Name         string
	Fingerprint  string
	Status       string
	Version      string
	Capabilities []string
	Remote       string
	PairedBy     string
	CreatedAt    time.Time
	LastSeenAt   *time.Time
	RevokedAt    *time.Time
	Connected    bool
}

// Token is a new pairing token with what the pairing dialog shows.
type Token struct {
	Token            string
	ExpiresAt        time.Time
	PanelAddr        string
	PanelFingerprint string
	Compose          string
}

// Service administers agents.
type Service struct {
	st         *store.Store
	log        *slog.Logger
	hub        Hub
	publicAddr string
	now        func() time.Time
}

// NewService returns the agent administration; publicAddr is
// RCD_AGENT_PUBLIC_ADDR.
func NewService(st *store.Store, log *slog.Logger, publicAddr string) *Service {
	return &Service{st: st, log: log, publicAddr: publicAddr, now: time.Now}
}

// SetHub connects the agent hub, which in turn uses the service as its
// registry.
func (s *Service) SetHub(h Hub) { s.hub = h }

func (s *Service) mustBeAdmin(ctx context.Context, req users.Request, action, target string) error {
	if req.Actor.Role == perm.RCONAdmin {
		return nil
	}
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: "forbidden", Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "agent", TargetID: target, Action: action, Result: audit.ResultDenied, Reason: "forbidden",
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
	return users.ErrForbidden
}

func (s *Service) auditOK(ctx context.Context, db sqlitedb.DBTX, req users.Request, action, target string, payload any) error {
	_, err := audit.Write(ctx, db, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "agent", TargetID: target, Action: action, Payload: payload, Result: audit.ResultOK,
	})
	return err
}

func toAgent(r sqlitedb.Agent, connected bool) Agent {
	a := Agent{
		ID: r.ID, Name: r.Name, Fingerprint: r.Fingerprint, Status: r.Status, Version: r.Version,
		Remote: r.Remote, PairedBy: r.PairedBy.String, Connected: connected && r.Status == StatusActive,
	}
	_ = json.Unmarshal([]byte(r.Capabilities), &a.Capabilities)
	if a.Capabilities == nil {
		a.Capabilities = []string{}
	}
	a.CreatedAt, _ = time.Parse(store.TimeLayout, r.CreatedAt)
	a.LastSeenAt = parseTime(r.LastSeenAt)
	a.RevokedAt = parseTime(r.RevokedAt)
	return a
}

func parseTime(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t, err := time.Parse(store.TimeLayout, v.String)
	if err != nil {
		return nil
	}
	return &t
}

func (s *Service) connected(id string) bool { return s.hub != nil && s.hub.Connected(id) }

// List returns all agents; pending ones older than 24 h are dropped first.
func (s *Service) List(ctx context.Context, req users.Request) ([]Agent, error) {
	if err := s.mustBeAdmin(ctx, req, "agent.read", ""); err != nil {
		return nil, err
	}
	if err := s.dropStale(ctx); err != nil {
		return nil, err
	}
	rows, err := s.st.Queries.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAgent(r, s.connected(r.ID)))
	}
	return out, nil
}

// Get returns one agent.
func (s *Service) Get(ctx context.Context, req users.Request, id string) (Agent, error) {
	if err := s.mustBeAdmin(ctx, req, "agent.read", id); err != nil {
		return Agent{}, err
	}
	r, err := s.st.Queries.GetAgent(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, users.ErrNotFound
	}
	if err != nil {
		return Agent{}, err
	}
	return toAgent(r, s.connected(id)), nil
}

func (s *Service) dropStale(ctx context.Context) error {
	return s.st.Queries.DeleteStalePendingAgents(ctx, s.now().Add(-PendingTimeout).UTC().Format(store.TimeLayout))
}

// CreateToken creates a pairing token, valid once for 24 h (way A).
func (s *Service) CreateToken(ctx context.Context, req users.Request) (Token, error) {
	if err := s.mustBeAdmin(ctx, req, "agent.token.create", ""); err != nil {
		return Token{}, err
	}
	if s.hub == nil {
		return Token{}, errors.New("agent hub not running")
	}
	pt, err := agentproto.NewPairingToken(s.publicAddr, s.hub.Fingerprint())
	if err != nil {
		return Token{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return Token{}, err
	}
	now := s.now().UTC()
	expires := now.Add(TokenLifetime)
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Token{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).InsertAgentToken(ctx, sqlitedb.InsertAgentTokenParams{
		ID: id, TokenHash: agentproto.SecretHash(pt.Secret), CreatedBy: req.Actor.ID,
		CreatedAt: now.Format(store.TimeLayout), ExpiresAt: expires.Format(store.TimeLayout),
	}); err != nil {
		return Token{}, err
	}
	// The token itself never goes into the audit or the log (SEC-012).
	if err := s.auditOK(ctx, tx, req, "agent.token.create", "", map[string]any{"token_id": id, "expires_at": expires}); err != nil {
		return Token{}, err
	}
	if err := tx.Commit(); err != nil {
		return Token{}, err
	}
	tok := pt.Encode()
	return Token{Token: tok, ExpiresAt: expires, PanelAddr: s.publicAddr, PanelFingerprint: pt.Fingerprint, Compose: compose(tok)}, nil
}

// compose is the agent service for the pairing dialog (AGT-005). The token
// is a secret: the dialog shows it once.
func compose(token string) string {
	version := buildinfo.Version
	if strings.HasPrefix(version, "0.0.0") {
		version = "latest"
	}
	return fmt.Sprintf(`services:
  rconductor-agent:
    image: ghcr.io/nightdragon1/rconductor-agent:%s
    restart: unless-stopped
    # GID of /var/run/docker.sock on the host: stat -c %%g /var/run/docker.sock
    group_add: ["DOCKER_GID"]
    environment:
      RCD_PAIRING_TOKEN: "%s"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - rconductor-agent-secrets:/secrets
volumes:
  rconductor-agent-secrets:
`, version, token)
}

// Approve accepts a pending pairing by fingerprint (way B).
func (s *Service) Approve(ctx context.Context, req users.Request, id string) (Agent, error) {
	if err := s.mustBeAdmin(ctx, req, "agent.approve", id); err != nil {
		return Agent{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	n, err := q.ActivateAgent(ctx, sqlitedb.ActivateAgentParams{PairedBy: sql.NullString{String: req.Actor.ID, Valid: true}, ID: id})
	if err != nil {
		return Agent{}, err
	}
	if n == 0 {
		return Agent{}, users.ErrNotFound
	}
	r, err := q.GetAgent(ctx, id)
	if err != nil {
		return Agent{}, err
	}
	if err := s.auditOK(ctx, tx, req, "agent.approve", id, map[string]string{"name": r.Name, "fingerprint": r.Fingerprint}); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(); err != nil {
		return Agent{}, err
	}
	s.log.Info("agent paired by fingerprint", logging.Event(logging.AgentPaired),
		slog.String("agent_id", id), slog.String("fingerprint", r.Fingerprint), slog.String(logging.KeyRequestID, req.RequestID))
	return toAgent(r, s.connected(id)), nil
}

// Delete revokes an active agent (Design 1.7) or turns down a pending
// pairing. A revoked agent stays listed and cannot connect again.
func (s *Service) Delete(ctx context.Context, req users.Request, id string) error {
	if err := s.mustBeAdmin(ctx, req, "agent.revoke", id); err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	r, err := q.GetAgent(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return users.ErrNotFound
	}
	if err != nil {
		return err
	}
	action := "agent.revoke"
	var detached int64
	switch r.Status {
	case StatusPending:
		action = "agent.reject"
		if _, err := q.DeletePendingAgent(ctx, id); err != nil {
			return err
		}
	case StatusActive:
		if _, err := q.RevokeAgent(ctx, sqlitedb.RevokeAgentParams{RevokedAt: sql.NullString{String: store.Now(), Valid: true}, ID: id}); err != nil {
			return err
		}
		// Its servers lose agent and container, as when the agent row
		// goes (ON DELETE SET NULL, revision 2.3).
		if detached, err = q.DetachServersFromAgent(ctx, sqlitedb.DetachServersFromAgentParams{
			UpdatedAt: store.Now(), AgentID: sql.NullString{String: id, Valid: true},
		}); err != nil {
			return err
		}
	default:
		return users.ErrNotFound
	}
	if err := s.auditOK(ctx, tx, req, action, id, map[string]any{"name": r.Name, "fingerprint": r.Fingerprint, "servers_detached": detached}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if action == "agent.revoke" {
		s.log.Info("agent revoked", logging.Event(logging.AgentRevoked),
			slog.String("agent_id", id), slog.String(logging.KeyRequestID, req.RequestID))
		if s.hub != nil {
			s.hub.Revoke(id)
		}
	}
	return nil
}

// PairingOpen reports whether pairing by fingerprint is switched on and
// not yet past its 30 minutes (revision 2.3 candidate 3).
func (s *Service) PairingOpen(ctx context.Context) bool {
	var on bool
	since, err := s.st.GlobalSettingSince(ctx, users.SettingFingerprintPairing, &on)
	return err == nil && on && s.now().Sub(since) < users.PairingWindow
}

// ClosePairing switches pairing by fingerprint off once its window has
// passed, with an audit entry; it runs every minute until ctx ends.
func (s *Service) ClosePairing(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.closeExpiredPairing(ctx); err != nil {
				s.log.Error("pairing window not closed", slog.Any("err", err))
			}
		}
	}
}

func (s *Service) closeExpiredPairing(ctx context.Context) error {
	var on bool
	since, err := s.st.GlobalSettingSince(ctx, users.SettingFingerprintPairing, &on)
	if err != nil || !on || s.now().Sub(since) < users.PairingWindow {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).SetGlobalSetting(ctx, sqlitedb.SetGlobalSettingParams{
		Value: "false", UpdatedAt: store.Now(), Key: users.SettingFingerprintPairing,
	}); err != nil {
		return err
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, TargetType: "settings", Action: "settings.update", Result: audit.ResultOK,
		Payload: map[string]any{users.SettingFingerprintPairing: map[string]any{"from": true, "to": false}, "reason": "window_expired"},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("pairing by fingerprint closed after its window", slog.Duration("window", users.PairingWindow))
	return nil
}

// Admit decides about a connecting agent (agenthub.Registry).
func (s *Service) Admit(ctx context.Context, h agentproto.Hello, key ed25519.PublicKey, remote string) (agenthub.Admission, error) {
	fp := keys.Fingerprint(key)
	switch {
	case h.Token != "":
		return s.pairByToken(ctx, h, key, fp, remote)
	case h.AgentID != "":
		r, err := s.st.Queries.GetAgent(ctx, h.AgentID)
		if errors.Is(err, sql.ErrNoRows) {
			return agenthub.Admission{Reject: agentproto.RejectUnknown}, nil
		}
		if err != nil {
			return agenthub.Admission{}, err
		}
		if !bytes.Equal(r.PublicKey, key) {
			return agenthub.Admission{PinMismatch: true}, nil
		}
		return s.admitKnown(ctx, r, h, remote)
	}
	// Way B: the agent knows only the panel address.
	r, err := s.st.Queries.GetAgentByFingerprint(ctx, fp)
	switch {
	case err == nil && r.Status == StatusRevoked && s.PairingOpen(ctx):
		// A revoked agent asks for a new pairing by fingerprint.
		now := sql.NullString{String: store.Now(), Valid: true}
		if _, err := s.st.Queries.RequestRepairAgent(ctx, sqlitedb.RequestRepairAgentParams{CreatedAt: now.String, LastSeenAt: now, ID: r.ID}); err != nil {
			return agenthub.Admission{}, err
		}
		return agenthub.Admission{Reject: agentproto.RejectPending}, nil
	case err == nil:
		return s.admitKnown(ctx, r, h, remote)
	case !errors.Is(err, sql.ErrNoRows):
		return agenthub.Admission{}, err
	}
	if !s.PairingOpen(ctx) {
		return agenthub.Admission{Reject: agentproto.RejectUnknown}, nil
	}
	return s.addPending(ctx, h, key, fp, remote)
}

func (s *Service) admitKnown(ctx context.Context, r sqlitedb.Agent, h agentproto.Hello, remote string) (agenthub.Admission, error) {
	switch r.Status {
	case StatusRevoked:
		return agenthub.Admission{Reject: agentproto.RejectRevoked}, nil
	case StatusPending:
		return agenthub.Admission{Reject: agentproto.RejectPending}, nil
	}
	caps, _ := json.Marshal(h.Capabilities)
	if err := s.st.Queries.TouchAgent(ctx, sqlitedb.TouchAgentParams{
		Name: h.Name, Version: h.Version, Capabilities: string(caps), Remote: remote,
		LastSeenAt: sql.NullString{String: store.Now(), Valid: true}, ID: r.ID,
	}); err != nil {
		return agenthub.Admission{}, err
	}
	return agenthub.Admission{AgentID: r.ID}, nil
}

func (s *Service) pairByToken(ctx context.Context, h agentproto.Hello, key ed25519.PublicKey, fp, remote string) (agenthub.Admission, error) {
	tok, err := s.st.Queries.GetAgentTokenByHash(ctx, agentproto.SecretHash(h.Token))
	if errors.Is(err, sql.ErrNoRows) {
		return agenthub.Admission{Reject: agentproto.RejectUnknown}, nil
	}
	if err != nil {
		return agenthub.Admission{}, err
	}
	// A key that is paired cannot pair twice; a revoked one pairs again
	// with its old record (Design 1.7: a new pairing needs way A or B).
	existing, err := s.st.Queries.GetAgentByFingerprint(ctx, fp)
	switch {
	case err == nil && existing.Status != StatusRevoked:
		return agenthub.Admission{Reject: agentproto.RejectInvalid}, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return agenthub.Admission{}, err
	}
	now := store.Now()
	caps, _ := json.Marshal(h.Capabilities)
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return agenthub.Admission{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	pairedBy := sql.NullString{String: tok.CreatedBy, Valid: true}
	seen := sql.NullString{String: now, Valid: true}
	id := existing.ID
	if id != "" {
		if _, err := q.RepairAgent(ctx, sqlitedb.RepairAgentParams{
			PairedBy: pairedBy, Name: h.Name, Version: h.Version, Capabilities: string(caps), Remote: remote, LastSeenAt: seen, ID: id,
		}); err != nil {
			return agenthub.Admission{}, err
		}
	} else {
		if id, err = store.NewID(); err != nil {
			return agenthub.Admission{}, err
		}
		if err := q.InsertAgent(ctx, sqlitedb.InsertAgentParams{
			ID: id, Name: h.Name, PublicKey: key, Fingerprint: fp, Status: StatusActive, Version: h.Version,
			Capabilities: string(caps), Remote: remote, PairedBy: pairedBy, CreatedAt: now, LastSeenAt: seen,
		}); err != nil {
			return agenthub.Admission{}, err
		}
	}
	n, err := q.UseAgentToken(ctx, sqlitedb.UseAgentTokenParams{
		UsedAt: sql.NullString{String: now, Valid: true}, AgentID: sql.NullString{String: id, Valid: true},
		ID: tok.ID, ExpiresAt: now,
	})
	if err != nil {
		return agenthub.Admission{}, err
	}
	if n == 0 {
		// Used or expired: same answer as an unknown token.
		return agenthub.Admission{Reject: agentproto.RejectUnknown}, nil
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAgent, ActorID: tok.CreatedBy, TargetType: "agent", TargetID: id, Action: "agent.pair",
		Payload: map[string]string{"name": h.Name, "fingerprint": fp, "token_id": tok.ID, "remote": remote}, Result: audit.ResultOK,
	}); err != nil {
		return agenthub.Admission{}, err
	}
	if err := tx.Commit(); err != nil {
		return agenthub.Admission{}, err
	}
	return agenthub.Admission{AgentID: id, Paired: true}, nil
}

func (s *Service) addPending(ctx context.Context, h agentproto.Hello, key ed25519.PublicKey, fp, remote string) (agenthub.Admission, error) {
	if err := s.dropStale(ctx); err != nil {
		return agenthub.Admission{}, err
	}
	n, err := s.st.Queries.CountPendingAgents(ctx)
	if err != nil {
		return agenthub.Admission{}, err
	}
	if n >= PendingMax {
		s.log.Warn("too many pairings wait for approval", logging.Event(logging.AgentLimit),
			slog.String(logging.KeyReason, "pending_pairings"), slog.String("fingerprint", fp), slog.String("remote", remote))
		return agenthub.Admission{Reject: agentproto.RejectUnknown}, nil
	}
	id, err := store.NewID()
	if err != nil {
		return agenthub.Admission{}, err
	}
	caps, _ := json.Marshal(h.Capabilities)
	now := store.Now()
	if err := s.st.Queries.InsertAgent(ctx, sqlitedb.InsertAgentParams{
		ID: id, Name: h.Name, PublicKey: key, Fingerprint: fp, Status: StatusPending, Version: h.Version,
		Capabilities: string(caps), Remote: remote, CreatedAt: now, LastSeenAt: sql.NullString{String: now, Valid: true},
	}); err != nil {
		return agenthub.Admission{}, err
	}
	s.log.Info("agent waits for approval", slog.String("agent_id", id), slog.String("fingerprint", fp), slog.String("remote", remote))
	return agenthub.Admission{Reject: agentproto.RejectPending}, nil
}
