// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Lines of a log tail (revision 2.4 candidate 20).
const (
	DefaultTailLines = 200
	MaxTailLines     = agentproto.MaxLogLines
	tailTimeout      = 15 * time.Second
)

// LogTail holds the last log lines of a server's container, oldest first;
// they are foreign data, cleaned and bounded (SEC-006).
type LogTail struct {
	ServerID  string
	Container string
	Lines     []string
	Truncated bool
}

// Logs reads the last n log lines of the server's container over the
// agent (logs.tail, UI-009): RCON admin everywhere, server admin in his
// scope, only with a connected agent (AGT-008). The role is checked before
// the server (revision 2.4 candidate 14); the agent's rate limit per
// container applies.
func (s *Service) Logs(ctx context.Context, req users.Request, id string, n int) (LogTail, error) {
	if req.Actor.Role != perm.RCONAdmin && req.Actor.Role != perm.ServerAdmin {
		s.denied(ctx, req, "server.logs", "server", id, "forbidden")
		return LogTail{}, users.ErrForbidden
	}
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return LogTail{}, err
	}
	sr, ok := rights.Server(id)
	if !ok {
		s.denied(ctx, req, "server.logs", "server", id, "not_found")
		return LogTail{}, users.ErrNotFound
	}
	if !sr.Agent {
		s.denied(ctx, req, "server.logs", "server", id, "forbidden")
		return LogTail{}, users.ErrForbidden
	}
	r, err := s.st.Queries.GetServer(ctx, id)
	if err != nil {
		return LogTail{}, err
	}
	if !r.AgentID.Valid || !r.ContainerName.Valid || s.agents == nil {
		return LogTail{}, ErrAgentUnavailable
	}
	n = min(max(n, 1), MaxTailLines)
	agentID, container := r.AgentID.String, r.ContainerName.String
	var got agentproto.LogLines
	collect := func(e agentproto.Envelope) {
		if e.Type != agentproto.TypeLogLines {
			return
		}
		if ll, err := agentproto.Payload[agentproto.LogLines](e); err == nil {
			// Bounded while collecting: an agent decides how much it sends.
			lines, cut := agentproto.LimitLines(append(got.Lines, ll.Lines...))
			got.Lines = lines
			got.Truncated = got.Truncated || ll.Truncated || cut
		}
	}
	tctx, cancel := context.WithTimeout(ctx, tailTimeout)
	defer cancel()
	res, err := s.agents.RequestData(tctx, agentID, agentproto.TypeLogsTail, agentproto.LogsTail{Container: container, Lines: n}, collect)
	switch {
	case errors.Is(err, agenthub.ErrNotCapable):
		s.denied(ctx, req, "server.logs", "server", id, ReasonAgentUnsupported)
		return LogTail{}, ErrAgentUnsupported
	case errors.Is(err, agenthub.ErrNotConnected), errors.Is(err, agenthub.ErrBusy):
		return LogTail{}, ErrAgentUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		return LogTail{}, ErrActionTimeout
	case err != nil:
		return LogTail{}, err
	}
	switch res.Code {
	case agentproto.CodeOK:
	case agentproto.CodeRateLimited:
		return LogTail{}, ErrActionRateLimited
	case agentproto.CodeTimeout:
		return LogTail{}, ErrActionTimeout
	default:
		return LogTail{}, &ActionFailedError{Code: res.Code}
	}
	// The agent's lines are foreign data: cleaned and bounded again here.
	lines, cut := agentproto.LimitLines(got.Lines)
	if len(lines) > n {
		lines, cut = lines[len(lines)-n:], true
	}
	s.log.Info("container logs read", logging.Event(logging.LogsTail), slog.String(logging.KeyRequestID, req.RequestID),
		slog.String(logging.KeyServer, id), slog.String("agent_id", agentID), slog.String("container", container),
		slog.Int("lines", len(lines)))
	return LogTail{ServerID: id, Container: container, Lines: lines, Truncated: cut || got.Truncated}, nil
}
