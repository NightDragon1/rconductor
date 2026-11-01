// SPDX-License-Identifier: AGPL-3.0-or-later

// Package audit writes the audit log (AUD-001): who did what, when, on which
// server or object, with which result, including rejections. Entries live
// in the same database as the data they describe (D-029); reading and
// retention follow with AUD-003 and AUD-004.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Kind is the area of an entry (Design 4.2).
type Kind string

// Kinds of entries.
const (
	KindCommand Kind = "command"
	KindAgent   Kind = "agent"
	KindAdmin   Kind = "admin"
	KindAuth    Kind = "auth"
)

// Result is the outcome of the audited action.
type Result string

// Results of an action.
const (
	ResultOK      Result = "ok"
	ResultDenied  Result = "denied"
	ResultError   Result = "error"
	ResultTimeout Result = "timeout"
)

// Entry is one audit record. Empty strings are stored as NULL. Payload is
// stored as JSON and must never contain secrets (SEC-012).
type Entry struct {
	// ID defaults to a new UUIDv7; a command passes its request ID
	// (Design 4.1).
	ID         string
	Kind       Kind
	ActorID    string
	ActorRole  string
	SessionID  string // session hash, never the session ID itself
	ServerID   string
	TargetType string
	TargetID   string
	Action     string
	Payload    any
	Result     Result
	Reason     string // reason code as in the API (Design 3.5)
}

// Write stores e through db, which may be a transaction so that a change
// and its audit entry commit together. It returns the entry ID.
func Write(ctx context.Context, db sqlitedb.DBTX, e Entry) (string, error) {
	if e.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return "", err
		}
		e.ID = id
	}
	payload := []byte("{}")
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return "", fmt.Errorf("encode audit payload: %w", err)
		}
		payload = b
	}
	err := sqlitedb.New(db).InsertAudit(ctx, sqlitedb.InsertAuditParams{
		ID:         e.ID,
		Ts:         store.Now(),
		Kind:       string(e.Kind),
		ActorID:    null(e.ActorID),
		ActorRole:  null(e.ActorRole),
		SessionID:  null(e.SessionID),
		ServerID:   null(e.ServerID),
		TargetType: null(e.TargetType),
		TargetID:   null(e.TargetID),
		Action:     e.Action,
		Payload:    string(payload),
		Result:     string(e.Result),
		ReasonCode: null(e.Reason),
	})
	if err != nil {
		return "", fmt.Errorf("write audit entry %s: %w", e.Action, err)
	}
	return e.ID, nil
}

func null(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
