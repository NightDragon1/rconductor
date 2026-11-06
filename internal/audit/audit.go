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
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
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

// writeWait is how long an entry outside a transaction tries to get written.
// It is far above the busy timeout of the database: nobody is left to tell
// about a failure, and under load many writers wait for each other (V-3).
var writeWait = 2 * time.Minute // a variable for the tests

const busyPause = 50 * time.Millisecond

// writers lets one entry at a time ask the database, so that they queue here
// instead of competing for the lock inside SQLite, which is not fair.
var writers = make(chan struct{}, 1)

// lostLog receives an entry the database did not take in time.
var lostLog atomic.Pointer[slog.Logger]

// SetLostLog names the log of last resort: an entry the database does not
// take within writeWait goes there in full, as audit.entry_lost, so that it
// is not gone. Payloads hold no secrets (SEC-012).
func SetLostLog(log *slog.Logger) { lostLog.Store(log) }

func logLost(params sqlitedb.InsertAuditParams, cause error) {
	log := lostLog.Load()
	if log == nil {
		return
	}
	log.Error("audit entry not written, kept here", logging.Event(logging.AuditLost),
		slog.String("id", params.ID), slog.String("ts", params.Ts), slog.String("kind", params.Kind),
		slog.String("actor_id", params.ActorID.String), slog.String("actor_role", params.ActorRole.String),
		slog.String("session_hash", params.SessionID.String), slog.String("server_id", params.ServerID.String),
		slog.String("target_type", params.TargetType.String), slog.String("target_id", params.TargetID.String),
		slog.String("action", params.Action), slog.String("payload", params.Payload),
		slog.String("result", params.Result), slog.String("reason", params.ReasonCode.String),
		slog.Any("err", cause))
}

// Write stores e through db, which may be a transaction so that a change
// and its audit entry commit together. It returns the entry ID. Outside a
// transaction the entry is written even if the client has hung up, and a
// locked database is retried: a refused attempt under load is what the log is
// for.
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
	params := sqlitedb.InsertAuditParams{
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
	}
	var err error
	if _, direct := db.(*sql.DB); direct {
		if err = insertRetrying(ctx, db, params); err != nil {
			logLost(params, err)
		}
	} else {
		err = sqlitedb.New(db).InsertAudit(ctx, params)
	}
	if err != nil {
		return "", fmt.Errorf("write audit entry %s: %w", e.Action, err)
	}
	return e.ID, nil
}

func insertRetrying(ctx context.Context, db sqlitedb.DBTX, params sqlitedb.InsertAuditParams) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeWait)
	defer cancel()
	select {
	case writers <- struct{}{}:
		defer func() { <-writers }()
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		err := sqlitedb.New(db).InsertAudit(ctx, params)
		if !store.IsBusy(err) {
			return err
		}
		select {
		case <-time.After(busyPause):
		case <-ctx.Done():
			return err
		}
	}
}

func null(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
