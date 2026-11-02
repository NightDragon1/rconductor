// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Timeout bounds the wait for a game server's answer.
const Timeout = 10 * time.Second

// MaxStoredResponse bounds the answer kept in the audit log, which is
// also the console history (D-029, SEC-006).
const MaxStoredResponse = 64 << 10

// Results of a command in audit and history.
const (
	ResultOK      = string(audit.ResultOK)
	ResultDenied  = string(audit.ResultDenied)
	ResultError   = string(audit.ResultError)
	ResultTimeout = string(audit.ResultTimeout)
)

// Settings keys of the rate limit (Design 3.4, 5.6): ratelimit.<role>.per_minute
// and ratelimit.<role>.burst.
func settingPerMinute(role string) string { return "ratelimit." + role + ".per_minute" }
func settingBurst(role string) string     { return "ratelimit." + role + ".burst" }

// SettingKeys lists the rate limit keys for the settings endpoint.
func SettingKeys() []string {
	var out []string
	for _, r := range perm.Roles {
		out = append(out, settingPerMinute(string(r)), settingBurst(string(r)))
	}
	return out
}

// EnsureDefaults stores the default rate limits unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	for role, lim := range DefaultLimits {
		if _, err := st.EnsureDefault(ctx, settingPerMinute(role), lim.PerMinute); err != nil {
			return err
		}
		if _, err := st.EnsureDefault(ctx, settingBurst(role), lim.Burst); err != nil {
			return err
		}
	}
	return nil
}

// Executor sends a command over the one connection of a server.
type Executor interface {
	Execute(ctx context.Context, serverID, command string) (string, error)
}

// DeniedError is a refused command with its reason code (Design 3.5).
type DeniedError struct {
	Reason     string
	RetryAfter time.Duration
}

func (e *DeniedError) Error() string { return "command refused: " + e.Reason }

var (
	// ErrUnavailable means there is no RCON connection (server_unavailable).
	ErrUnavailable = errors.New("no rcon connection to the game server")
	// ErrTimeout means the game server did not answer in time.
	ErrTimeout = errors.New("the game server did not answer in time")
)

// Result is a sent command with its answer, marked with its request ID
// (CMD-007).
type Result struct {
	RequestID string
	TS        time.Time
	Command   string
	Response  string
	Truncated bool
}

// Entry is one command of the history: a block of the console (CMD-012).
type Entry struct {
	RequestID string
	TS        time.Time
	UserID    string
	Pseudonym string
	Role      string
	Command   string
	Response  string
	Truncated bool
	Result    string
	Reason    string
}

// Service runs the pipeline.
type Service struct {
	st      *store.Store
	perm    *perm.Checker
	exec    Executor
	log     *slog.Logger
	limiter *limiter
	now     func() time.Time
	// Publish, if set, receives every finished or refused command for
	// the live console (event bus).
	Publish func(serverID string, e Entry)
}

// NewService returns the command pipeline.
func NewService(st *store.Store, c *perm.Checker, exec Executor, log *slog.Logger) *Service {
	return &Service{st: st, perm: c, exec: exec, log: log, limiter: newLimiter(), now: time.Now}
}

type payload struct {
	Command   string `json:"command"`
	Response  string `json:"response,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (s *Service) limit(ctx context.Context, role string) Limit {
	lim := DefaultLimits[role]
	var n int
	if err := s.st.GlobalSetting(ctx, settingPerMinute(role), &n); err == nil && n > 0 {
		lim.PerMinute = n
	}
	if err := s.st.GlobalSetting(ctx, settingBurst(role), &n); err == nil && n > 0 {
		lim.Burst = n
	}
	return lim
}

func (s *Service) syntax(ctx context.Context, serverID string) (Syntax, error) {
	r, err := s.st.Queries.GetServer(ctx, serverID)
	if err != nil {
		return Syntax{}, err
	}
	syn := Syntax{StripPrefix: "/", MaxLength: profiles.DefaultMaxLength}
	if !r.ProfileID.Valid {
		return syn, nil
	}
	gp, err := s.st.Queries.GetGameProfile(ctx, r.ProfileID.String)
	if err != nil {
		return syn, nil //nolint:nilerr // a missing profile falls back to the defaults
	}
	var rules profiles.Profile
	if err := json.Unmarshal([]byte(gp.Rules), &rules); err != nil {
		return syn, nil //nolint:nilerr // see above
	}
	if rules.Syntax.CaseSensitive != nil {
		syn.CaseSensitive = *rules.Syntax.CaseSensitive
	}
	if rules.Syntax.StripPrefix != nil {
		syn.StripPrefix = *rules.Syntax.StripPrefix
	}
	if rules.Syntax.MaxLength > 0 {
		syn.MaxLength = rules.Syntax.MaxLength
	}
	syn.Wrappers = rules.Syntax.Wrappers
	return syn, nil
}

func (s *Service) attrs(req users.Request, serverID string) []any {
	return []any{
		slog.String(logging.KeyRequestID, req.RequestID), slog.String(logging.KeySession, req.SessionRef),
		slog.String(logging.KeyUser, req.Pseudonym), slog.String(logging.KeyRole, string(req.Actor.Role)),
		slog.String(logging.KeyServer, serverID),
	}
}

func (s *Service) entry(ctx context.Context, req users.Request, serverID string, e audit.Entry) error {
	e.ID, e.Kind, e.Action = req.RequestID, audit.KindCommand, "command"
	e.ActorID, e.ActorRole, e.SessionID, e.ServerID = req.Actor.ID, string(req.Actor.Role), req.SessionID, serverID
	_, err := audit.Write(ctx, s.st.DB, e)
	return err
}

// deny logs cmd.denied, audits the refusal and tells the console.
func (s *Service) deny(ctx context.Context, req users.Request, serverID, command, reason string, retry time.Duration) error {
	s.log.Info("command refused", append(s.attrs(req, serverID), logging.Event(logging.CmdDenied), slog.String(logging.KeyReason, reason))...)
	if err := s.entry(ctx, req, serverID, audit.Entry{
		Payload: payload{Command: command}, Result: audit.ResultDenied, Reason: reason,
	}); err != nil {
		return err
	}
	s.publish(serverID, Entry{
		RequestID: req.RequestID, TS: s.now().UTC(), UserID: req.Actor.ID, Pseudonym: req.Pseudonym,
		Role: string(req.Actor.Role), Command: command, Result: ResultDenied, Reason: reason,
	})
	return &DeniedError{Reason: reason, RetryAfter: retry}
}

func (s *Service) publish(serverID string, e Entry) {
	if s.Publish != nil {
		s.Publish(serverID, e)
	}
}

// Send runs one command through the pipeline of Design 3.3 and returns the
// game server's answer.
func (s *Service) Send(ctx context.Context, req users.Request, serverID, input string) (Result, error) {
	// 1 and 2: session and role are checked by the web layer; scope here.
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return Result{}, err
	}
	sr, ok := rights.Server(serverID)
	if !ok {
		perm.LogDenied(s.log, perm.Denial{
			RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym, Role: req.Actor.Role,
			Server: serverID, Reason: "not_found", Action: "command",
		})
		return Result{}, users.ErrNotFound
	}
	syn, err := s.syntax(ctx, serverID)
	if err != nil {
		return Result{}, err
	}
	// 3: cleaning, prefix, length (CMD-006, CAT-005).
	cmd, err := Clean(input, syn)
	if err != nil {
		return Result{}, s.deny(ctx, req, serverID, truncate(input, 256), ReasonInvalid, 0)
	}
	// 4: tokens.
	tokens := Tokens(cmd)
	// 5 to 7: only moderators and supporters are bound to lists
	// (CMD-002); wrappers always refused for them (CMD-005).
	if !sr.AllCommands {
		if IsWrapper(tokens, syn) {
			return Result{}, s.deny(ctx, req, serverID, cmd, ReasonWrapperBlocked, 0)
		}
		if !AllowedByList(tokens, sr.Commands, syn.CaseSensitive) {
			return Result{}, s.deny(ctx, req, serverID, cmd, ReasonNotInList, 0)
		}
	}
	// 8: rate limit (CMD-019).
	if ok, wait := s.limiter.take(req.Actor.ID, serverID, s.limit(ctx, string(req.Actor.Role)), s.now()); !ok {
		return Result{}, s.deny(ctx, req, serverID, cmd, ReasonRateLimited, wait)
	}
	// 9 and 10: marked with request ID, session, user and role; audit
	// before sending (CMD-007).
	s.log.Info("command accepted", append(s.attrs(req, serverID), logging.Event(logging.CmdAccepted))...)
	if err := s.entry(ctx, req, serverID, audit.Entry{Payload: payload{Command: cmd}, Result: audit.ResultError, Reason: "not_answered"}); err != nil {
		return Result{}, err
	}
	// 11: one connection, one queue per server.
	ectx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	s.log.Info("command sent", append(s.attrs(req, serverID), logging.Event(logging.CmdSent))...)
	start := s.now()
	answer, err := s.exec.Execute(ectx, serverID, cmd)
	res := Result{RequestID: req.RequestID, TS: start.UTC(), Command: cmd}
	outcome, reason := ResultOK, ""
	switch {
	case err == nil:
		res.Response = truncate(answer, MaxStoredResponse)
		res.Truncated = len(answer) > len(res.Response)
		s.log.Info("command answered", append(s.attrs(req, serverID), logging.Event(logging.CmdResponse),
			slog.Int("bytes", len(answer)), logging.Duration(s.now().Sub(start)))...)
	case errors.Is(err, context.DeadlineExceeded):
		outcome, reason = ResultTimeout, "timeout"
		s.log.Warn("command timed out", append(s.attrs(req, serverID), logging.Event(logging.CmdTimeout))...)
		err = ErrTimeout
	case errors.Is(err, connmgr.ErrUnavailable), errors.Is(err, connmgr.ErrUnknownServer):
		outcome, reason = ResultError, "server_unavailable"
		err = ErrUnavailable
	default:
		outcome, reason = ResultError, "server_unavailable"
		s.log.Warn("command failed", append(s.attrs(req, serverID), slog.Any("err", err))...)
		err = ErrUnavailable
	}
	// 12: result in the audit and to the console.
	body, _ := json.Marshal(payload{Command: cmd, Response: res.Response, Truncated: res.Truncated})
	if uerr := s.st.Queries.SetAuditResult(context.WithoutCancel(ctx), sqlitedb.SetAuditResultParams{
		Result: outcome, ReasonCode: sql.NullString{String: reason, Valid: reason != ""}, Payload: string(body), ID: req.RequestID,
	}); uerr != nil {
		s.log.Error("audit result not stored", slog.String(logging.KeyRequestID, req.RequestID), slog.Any("err", uerr))
	}
	s.publish(serverID, Entry{
		RequestID: req.RequestID, TS: res.TS, UserID: req.Actor.ID, Pseudonym: req.Pseudonym, Role: string(req.Actor.Role),
		Command: cmd, Response: res.Response, Truncated: res.Truncated, Result: outcome, Reason: reason,
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// truncate cuts s to at most n bytes at a character boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// History returns the commands of a server, newest first (Design 2.2):
// all for RCON admin and server admin, only the own ones for moderators
// and supporters in R1 (Design 6.3).
func (s *Service) History(ctx context.Context, req users.Request, serverID string, limit int, cursor string) ([]Entry, string, error) {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return nil, "", err
	}
	if _, ok := rights.Server(serverID); !ok {
		return nil, "", users.ErrNotFound
	}
	if limit <= 0 {
		limit = users.DefaultAuditLimit
	}
	limit = min(limit, users.MaxAuditLimit)
	cts, cid, err := users.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	actor := ""
	if rights.Role == perm.Moderator || rights.Role == perm.Supporter {
		actor = req.Actor.ID
	}
	rows, err := s.st.Queries.ListCommandHistory(ctx, sqlitedb.ListCommandHistoryParams{
		ServerID: sql.NullString{String: serverID, Valid: true}, ActorID: actor, CursorTs: cts, CursorID: cid, Lim: int64(limit) + 1,
	})
	if err != nil {
		return nil, "", fmt.Errorf("read history: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = users.EncodeCursor(rows[len(rows)-1].Ts, rows[len(rows)-1].ID)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		var p payload
		_ = json.Unmarshal([]byte(r.Payload), &p)
		e := Entry{
			RequestID: r.ID, UserID: r.ActorID.String, Pseudonym: r.Pseudonym, Role: r.ActorRole.String,
			Command: p.Command, Response: p.Response, Truncated: p.Truncated, Result: r.Result, Reason: r.ReasonCode.String,
		}
		e.TS, _ = time.Parse(store.TimeLayout, r.Ts)
		out = append(out, e)
	}
	return out, next, nil
}
