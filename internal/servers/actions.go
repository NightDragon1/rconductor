// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Agents is what the actions need from the agent hub.
type Agents interface {
	Request(ctx context.Context, agentID, typ string, payload any) (agentproto.Result, error)
}

// Executor runs an RCON command, for the restart without agent.
type Executor interface {
	Execute(ctx context.Context, serverID, command string) (string, error)
}

// SetActions links the agent hub and the connection manager for actions.
func (s *Service) SetActions(a Agents, rcon Executor) { s.agents, s.rcon = a, rcon }

// Timeouts of an action.
const (
	agentActionTimeout = 70 * time.Second // Docker stops with 10 s grace, restart takes longer
	rconActionTimeout  = 10 * time.Second
)

// Errors of an action (Design 3.5, revision 2.3 candidate 6).
var (
	// ErrAgentUnavailable means the server has no connected agent, or the
	// action needs one.
	ErrAgentUnavailable = errors.New("no connected agent for this server")
	// ErrRCONUnavailable means the restart without agent found no RCON
	// connection.
	ErrRCONUnavailable = errors.New("no rcon connection to the game server")
	// ErrActionTimeout means the agent or the game server did not answer.
	ErrActionTimeout = errors.New("the action did not finish in time")
	// ErrActionRateLimited means the agent's rate for the container is used up.
	ErrActionRateLimited = errors.New("too many actions for this container")
)

// ActionFailedError carries the agent's result code.
type ActionFailedError struct{ Code string }

func (e *ActionFailedError) Error() string { return "the agent refused the action: " + e.Code }

// ActionResult says how an action ran.
type ActionResult struct {
	Action string
	Via    string // agent or rcon
}

// Action starts, stops or restarts the container of a server (AGT-003):
// RCON admin everywhere, server admin in his scope (AGT-008). Without an
// agent only restart works, by the profile's stop command (AGT-009).
func (s *Service) Action(ctx context.Context, req users.Request, id, action string) (ActionResult, error) {
	if action != agentproto.ActionStart && action != agentproto.ActionStop && action != agentproto.ActionRestart {
		return ActionResult{}, &users.InvalidError{Detail: "action must be start, stop or restart"}
	}
	// Actions are for the RCON admin and server admins only (Design 2.2);
	// the role is checked before the server is looked up.
	if req.Actor.Role != perm.RCONAdmin && req.Actor.Role != perm.ServerAdmin {
		return ActionResult{}, s.actionForbidden(ctx, req, id, action)
	}
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return ActionResult{}, err
	}
	sr, ok := rights.Server(id)
	if !ok {
		s.denied(ctx, req, "server.action", "server", id, "not_found")
		return ActionResult{}, users.ErrNotFound
	}
	if !sr.Agent {
		return ActionResult{}, s.actionForbidden(ctx, req, id, action)
	}
	r, err := s.st.Queries.GetServer(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, users.ErrNotFound
	}
	if err != nil {
		return ActionResult{}, err
	}
	if r.AgentID.Valid && r.ContainerName.Valid {
		return s.agentAction(ctx, req, id, r.AgentID.String, r.ContainerName.String, action)
	}
	if action != agentproto.ActionRestart {
		s.actionAudit(ctx, req, id, action, "", audit.ResultError, "agent_unavailable", nil)
		return ActionResult{}, ErrAgentUnavailable
	}
	return s.rconRestart(ctx, req, id, r.ProfileID)
}

func (s *Service) actionForbidden(ctx context.Context, req users.Request, id, action string) error {
	s.actionAudit(ctx, req, id, action, "", audit.ResultDenied, "forbidden", nil)
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Server: id, Reason: "forbidden", Action: "server.action",
	})
	return users.ErrForbidden
}

func (s *Service) agentAction(ctx context.Context, req users.Request, id, agentID, container, action string) (ActionResult, error) {
	detail := map[string]string{"agent_id": agentID, "container": container}
	if s.agents == nil {
		s.actionAudit(ctx, req, id, action, "agent", audit.ResultError, "agent_unavailable", detail)
		return ActionResult{}, ErrAgentUnavailable
	}
	actx, cancel := context.WithTimeout(ctx, agentActionTimeout)
	defer cancel()
	res, err := s.agents.Request(actx, agentID, agentproto.TypeContainerAction, agentproto.ContainerAction{Container: container, Action: action})
	switch {
	case errors.Is(err, agenthub.ErrNotConnected), errors.Is(err, agenthub.ErrBusy):
		s.actionAudit(ctx, req, id, action, "agent", audit.ResultError, "agent_unavailable", detail)
		return ActionResult{}, ErrAgentUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		s.actionAudit(ctx, req, id, action, "agent", audit.ResultTimeout, "timeout", detail)
		return ActionResult{}, ErrActionTimeout
	case err != nil:
		return ActionResult{}, err
	}
	switch res.Code {
	case agentproto.CodeOK:
		s.actionAudit(ctx, req, id, action, "agent", audit.ResultOK, "", detail)
		s.log.Info("server action", slog.String(logging.KeyServer, id), slog.String("action", action), slog.String("via", "agent"),
			slog.String(logging.KeyRequestID, req.RequestID))
		return ActionResult{Action: action, Via: "agent"}, nil
	case agentproto.CodeRateLimited:
		s.actionAudit(ctx, req, id, action, "agent", audit.ResultDenied, "rate_limited", detail)
		return ActionResult{}, ErrActionRateLimited
	case agentproto.CodeTimeout:
		s.actionAudit(ctx, req, id, action, "agent", audit.ResultTimeout, "timeout", detail)
		return ActionResult{}, ErrActionTimeout
	}
	s.actionAudit(ctx, req, id, action, "agent", audit.ResultError, res.Code, detail)
	return ActionResult{}, &ActionFailedError{Code: res.Code}
}

// rconRestart sends the profile's stop command; the container comes back
// by its restart policy unless-stopped (AGT-009).
func (s *Service) rconRestart(ctx context.Context, req users.Request, id string, profileID sql.NullString) (ActionResult, error) {
	cmd := ""
	if profileID.Valid {
		if p, err := s.st.Queries.GetGameProfile(ctx, profileID.String); err == nil {
			var rules profiles.Profile
			if json.Unmarshal([]byte(p.Rules), &rules) == nil && rules.Stop != nil {
				cmd = rules.Stop.Command
			}
		}
	}
	if cmd == "" || s.rcon == nil {
		s.actionAudit(ctx, req, id, agentproto.ActionRestart, "rcon", audit.ResultError, "agent_unavailable", nil)
		return ActionResult{}, ErrAgentUnavailable
	}
	actx, cancel := context.WithTimeout(ctx, rconActionTimeout)
	defer cancel()
	_, err := s.rcon.Execute(actx, id, cmd)
	detail := map[string]string{"command": cmd}
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		s.actionAudit(ctx, req, id, agentproto.ActionRestart, "rcon", audit.ResultTimeout, "timeout", detail)
		return ActionResult{}, ErrActionTimeout
	case errors.Is(err, connmgr.ErrUnavailable), errors.Is(err, connmgr.ErrUnknownServer), errors.Is(err, connmgr.ErrQueueFull):
		s.actionAudit(ctx, req, id, agentproto.ActionRestart, "rcon", audit.ResultError, "server_unavailable", detail)
		return ActionResult{}, ErrRCONUnavailable
	default:
		// The game server may close the connection while it stops; the
		// command reached it.
		s.log.Debug("stop command ended with an error", slog.String(logging.KeyServer, id), slog.Any("err", err))
	}
	s.actionAudit(ctx, req, id, agentproto.ActionRestart, "rcon", audit.ResultOK, "", detail)
	s.log.Info("server action", slog.String(logging.KeyServer, id), slog.String("action", agentproto.ActionRestart),
		slog.String("via", "rcon"), slog.String(logging.KeyRequestID, req.RequestID))
	return ActionResult{Action: agentproto.ActionRestart, Via: "rcon"}, nil
}

// actionAudit writes one entry per action with its result (AUD-001).
func (s *Service) actionAudit(ctx context.Context, req users.Request, id, action, via string, result audit.Result, reason string, detail map[string]string) {
	payload := map[string]string{"action": action}
	if via != "" {
		payload["via"] = via
	}
	for k, v := range detail {
		payload[k] = v
	}
	if _, err := audit.Write(context.WithoutCancel(ctx), s.st.DB, audit.Entry{
		Kind: audit.KindAgent, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		ServerID: id, TargetType: "server", TargetID: id, Action: "server.action", Payload: payload, Result: result, Reason: reason,
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", "server.action"), slog.Any("err", fmt.Errorf("%s: %w", action, err)))
	}
}
