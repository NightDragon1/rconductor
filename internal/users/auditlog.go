// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Page sizes of the audit log.
const (
	DefaultAuditLimit = 50
	MaxAuditLimit     = 200
)

// AuditEntry is one entry as shown in the audit view. The session appears
// only as short hash (Design 8.2).
type AuditEntry struct {
	ID         string
	TS         time.Time
	Kind       string
	ActorID    string
	ActorRole  string
	SessionRef string
	ServerID   string
	TargetType string
	TargetID   string
	Action     string
	Payload    map[string]any
	Result     string
	Reason     string
}

// AuditQuery filters and pages the audit log.
type AuditQuery struct {
	Kind     string
	ServerID string
	Limit    int
	Cursor   string // opaque, from the previous page
}

// EncodeCursor makes an opaque cursor for paging newest first by (ts, id).
func EncodeCursor(ts, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ts + "|" + id))
}

// DecodeCursor reads a cursor of EncodeCursor; empty means the first page.
func DecodeCursor(c string) (ts, id string, err error) {
	if c == "" {
		return "", "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	ts, id, ok := strings.Cut(string(b), "|")
	if err != nil || !ok {
		return "", "", &InvalidError{Detail: "invalid cursor"}
	}
	return ts, id, nil
}

// Audit returns a page of the audit log (AUD-004): everything for the RCON
// admin; for a server admin the entries about servers in his scope, about
// users he manages and his own actions. Nobody else may read it.
func (s *Service) Audit(ctx context.Context, req Request, q AuditQuery) ([]AuditEntry, string, error) {
	if req.Actor.Role != perm.RCONAdmin && req.Actor.Role != perm.ServerAdmin {
		s.deniedOn(ctx, req, "audit.read", "audit", "", "forbidden", nil)
		return nil, "", ErrForbidden
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultAuditLimit
	}
	limit = min(limit, MaxAuditLimit)
	cts, cid, err := DecodeCursor(q.Cursor)
	if err != nil {
		return nil, "", err
	}

	var rows []sqlitedb.AuditLog
	if req.Actor.Role == perm.RCONAdmin {
		rows, err = s.st.Queries.ListAuditAll(ctx, sqlitedb.ListAuditAllParams{
			CursorTs: cts, CursorID: cid, Kind: q.Kind, ServerID: q.ServerID, Lim: int64(limit) + 1,
		})
	} else {
		var r *perm.Rights
		if r, err = s.perm.For(ctx, req.Actor.ID); err != nil {
			return nil, "", err
		}
		var managed map[string]bool
		if managed, err = s.perm.Managed(ctx, req.Actor); err != nil {
			return nil, "", err
		}
		servers, _ := json.Marshal(r.ServerIDs())
		users, _ := json.Marshal(keys(managed))
		rows, err = s.st.Queries.ListAuditScoped(ctx, sqlitedb.ListAuditScopedParams{
			CursorTs: cts, CursorID: cid, Kind: q.Kind, ServerID: q.ServerID,
			Servers: string(servers), Users: string(users),
			Self: sql.NullString{String: req.Actor.ID, Valid: true}, Lim: int64(limit) + 1,
		})
	}
	if err != nil {
		return nil, "", fmt.Errorf("read audit log: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = EncodeCursor(last.Ts, last.ID)
	}
	out := make([]AuditEntry, 0, len(rows))
	for _, r := range rows {
		e := AuditEntry{
			ID: r.ID, Kind: r.Kind, ActorID: r.ActorID.String, ActorRole: r.ActorRole.String,
			ServerID: r.ServerID.String, TargetType: r.TargetType.String, TargetID: r.TargetID.String,
			Action: r.Action, Result: r.Result, Reason: r.ReasonCode.String, Payload: map[string]any{},
		}
		if r.SessionID.Valid && len(r.SessionID.String) >= 8 {
			e.SessionRef = r.SessionID.String[:8]
		}
		e.TS, _ = time.Parse(store.TimeLayout, r.Ts)
		_ = json.Unmarshal([]byte(r.Payload), &e.Payload)
		if req.Actor.Role != perm.RCONAdmin && e.TargetType == "server" {
			// Host and RCON port of a server: the RCON admin only (D-056).
			delete(e.Payload, "host")
			delete(e.Payload, "port")
		}
		out = append(out, e)
	}
	return out, next, nil
}
