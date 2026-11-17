// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Bounds of the time budget of one batch of lines, a setting (UI-022).
const (
	MinBudgetMS     = 50
	DefaultBudgetMS = 250
	MaxBudgetMS     = 2000
)

// ErrUnreadable means the stored settings or texts cannot be read; the
// caller keeps what it read before (F-93).
var ErrUnreadable = errors.New("masking settings cannot be read")

// Settings holds the masking settings of the RCON admin (UI-022, D-088 c):
// one row of its own, its texts sealed with the master key (D-058).
type Settings struct {
	st  *store.Store
	box *secret.Box
	log *slog.Logger
}

// NewSettings returns the settings service; without a log it logs nothing.
func NewSettings(st *store.Store, box *secret.Box, log *slog.Logger) *Settings {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Settings{st: st, box: box, log: log}
}

// State is the stored setup; Texts are sealed, TextCount is in clear.
type State struct {
	Version   int64  `json:"version"`
	Enabled   bool   `json:"enabled"`
	MaxTexts  int    `json:"max_texts"`
	TextRunes int    `json:"max_text_chars"`
	BudgetMS  int    `json:"budget_ms"`
	TextCount int    `json:"text_count"`
	Texts     []byte `json:"texts,omitempty"`
}

// DefaultState is the setup before the first change: masking on.
func DefaultState() State {
	return State{Enabled: true, MaxTexts: DefaultTexts, TextRunes: DefaultTextRunes, BudgetMS: DefaultBudgetMS}
}

func (st State) valid() bool {
	return st.MaxTexts >= 1 && st.MaxTexts <= MaxTexts && st.TextRunes >= MinTextRunes && st.TextRunes <= MaxTextRunes &&
		st.BudgetMS >= MinBudgetMS && st.BudgetMS <= MaxBudgetMS && st.TextCount >= 0 && st.Version >= 0
}

// sameSetup reports whether a and b differ in nothing but their texts.
func sameSetup(a, b State) bool {
	return a.Enabled == b.Enabled && a.MaxTexts == b.MaxTexts && a.TextRunes == b.TextRunes && a.BudgetMS == b.BudgetMS
}

// SettingsInput is a change; nil fields stay, SetTexts replaces all texts.
// IfMatch is the version read before, nil without If-Match.
type SettingsInput struct {
	IfMatch   *int64
	Enabled   *bool
	SetTexts  bool
	Texts     []Text
	MaxTexts  *int
	TextRunes *int
	BudgetMS  *int
}

// complete reports whether the input sets every field and the texts.
func (in SettingsInput) complete() bool {
	return in.Enabled != nil && in.MaxTexts != nil && in.TextRunes != nil && in.BudgetMS != nil && in.SetTexts
}

// sealedText is a Text as it is sealed; Text itself never shows in JSON.
type sealedText struct {
	Value string
	Fold  bool
}

func textsContext() secret.Context {
	return secret.Context{Table: "settings", ID: users.SettingLogMasking, Column: "texts"}
}

// Load reads the stored setup; a missing row is the default.
func (s *Settings) Load(ctx context.Context) (State, error) { return s.load(ctx, s.st.Queries) }

func (s *Settings) load(ctx context.Context, q *sqlitedb.Queries) (State, error) {
	v, err := q.GetSetting(ctx, sqlitedb.GetSettingParams{Key: users.SettingLogMasking, ScopeType: "global"})
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultState(), nil
	}
	if ctx.Err() != nil {
		return DefaultState(), ctx.Err() // ended by the caller, not a read error
	}
	st := DefaultState()
	if err != nil || json.Unmarshal([]byte(v), &st) != nil || !st.valid() {
		s.readFailed()
		return DefaultState(), ErrUnreadable // protection on, never the zero value
	}
	return st, nil
}

// brokenVersion is the version of an unreadable row as far as it can be
// read, 0 otherwise, so that a repair never lowers it.
func brokenVersion(ctx context.Context, q *sqlitedb.Queries) int64 {
	v, err := q.GetSetting(ctx, sqlitedb.GetSettingParams{Key: users.SettingLogMasking, ScopeType: "global"})
	var head struct{ Version int64 }
	if err != nil || json.Unmarshal([]byte(v), &head) != nil || head.Version < 0 || head.Version >= math.MaxInt64-1 {
		return 0
	}
	return head.Version
}

// readFailed logs a read error of the settings, never their content (F-93).
func (s *Settings) readFailed() {
	s.log.Error("masking settings unreadable", logging.Event(logging.SettingReadFailed), slog.String("key", users.SettingLogMasking))
}

// OpenTexts opens the sealed texts of st; another master key or a broken
// value is ErrUnreadable.
func (s *Settings) OpenTexts(st State) ([]Text, error) {
	if len(st.Texts) == 0 {
		return nil, nil
	}
	if s.box == nil {
		return nil, ErrUnreadable
	}
	plain, err := s.box.Open(st.Texts, textsContext())
	var sealed []sealedText // the opened value is converted, as for the second factor
	if err != nil || json.Unmarshal([]byte(plain), &sealed) != nil {
		s.readFailed()
		return nil, ErrUnreadable
	}
	texts := make([]Text, len(sealed))
	for i, t := range sealed {
		texts[i] = Text(t)
	}
	return texts, nil
}

// Read returns the setup; RCON admin only.
func (s *Settings) Read(ctx context.Context, req users.Request) (State, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return State{}, s.denied(ctx, req, "log_masking.read")
	}
	return s.Load(ctx)
}

// Write changes the setup: the role (403), the version (428), the input
// (400), then the version against the stored one (409), as in D-051 and
// D-058. Errors name a text by its position only (SEC-012).
func (s *Settings) Write(ctx context.Context, req users.Request, in SettingsInput) (State, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return State{}, s.denied(ctx, req, "log_masking.updated")
	}
	if in.IfMatch == nil {
		return State{}, users.ErrPreconditionRequired
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	cur, err := s.load(ctx, q)
	repaired := false
	if errors.Is(err, ErrUnreadable) && in.complete() {
		// A write that sets every field repairs an unreadable row; it
		// needs a real version and never goes below the one stored.
		if *in.IfMatch < 0 || *in.IfMatch >= math.MaxInt64-1 {
			return State{}, users.ErrConflict
		}
		cur, repaired, err = DefaultState(), true, nil
		cur.Version = max(brokenVersion(ctx, q), *in.IfMatch)
	}
	if err != nil {
		return State{}, err
	}
	next, replaced, err := s.apply(cur, in)
	if err != nil {
		return State{}, err
	}
	if *in.IfMatch != cur.Version && !repaired {
		return State{}, users.ErrConflict
	}
	if !replaced && sameSetup(cur, next) {
		return cur, nil // {} or no change: same version, no audit entry
	}
	next.Version = cur.Version + 1
	value, err := json.Marshal(next)
	if err != nil {
		return State{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return State{}, err
	}
	if err := q.UpsertScopedSetting(ctx, sqlitedb.UpsertScopedSettingParams{
		ID: id, Key: users.SettingLogMasking, Value: string(value), ScopeType: "global", UpdatedAt: store.Now(),
	}); err != nil {
		return State{}, err
	}
	fromTo := func(a, b any) map[string]any { return map[string]any{"from": a, "to": b} }
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "settings", TargetID: "log_masking", Action: "log_masking.updated", Result: audit.ResultOK,
		Payload: map[string]any{
			"enabled": fromTo(cur.Enabled, next.Enabled), "text_count": fromTo(cur.TextCount, next.TextCount),
			"max_texts": fromTo(cur.MaxTexts, next.MaxTexts), "max_text_chars": fromTo(cur.TextRunes, next.TextRunes),
			"budget_ms": fromTo(cur.BudgetMS, next.BudgetMS), "texts_replaced": replaced, "repaired": repaired,
		},
	}); err != nil {
		return State{}, err
	}
	return next, tx.Commit()
}

// apply checks the input against the limits after the change, for texts
// sent and stored, and seals new texts.
func (s *Settings) apply(cur State, in SettingsInput) (State, bool, error) {
	next := cur
	if in.Enabled != nil {
		next.Enabled = *in.Enabled
	}
	for _, b := range []struct {
		v, dst *int
		lo, hi int
		name   string
	}{
		{in.MaxTexts, &next.MaxTexts, 1, MaxTexts, "max_texts"},
		{in.TextRunes, &next.TextRunes, MinTextRunes, MaxTextRunes, "max_text_chars"},
		{in.BudgetMS, &next.BudgetMS, MinBudgetMS, MaxBudgetMS, "budget_ms"},
	} {
		if b.v == nil {
			continue
		}
		if *b.v < b.lo || *b.v > b.hi {
			return State{}, false, &users.InvalidError{Detail: fmt.Sprintf("%s must be %d to %d", b.name, b.lo, b.hi)}
		}
		*b.dst = *b.v
	}
	texts := in.Texts
	if !in.SetTexts {
		stored, err := s.OpenTexts(cur)
		if err != nil {
			return State{}, false, err
		}
		texts = stored
	}
	if _, err := New(texts, Limits{Texts: next.MaxTexts, TextRunes: next.TextRunes}); err != nil {
		return State{}, false, &users.InvalidError{Detail: err.Error()}
	}
	if !in.SetTexts {
		return next, false, nil
	}
	next.TextCount, next.Texts = len(texts), nil
	if len(texts) > 0 {
		if s.box == nil {
			return State{}, false, ErrUnreadable
		}
		sealed := make([]sealedText, len(texts))
		for i, t := range texts {
			sealed[i] = sealedText(t)
		}
		plain, err := json.Marshal(sealed)
		if err != nil {
			return State{}, false, err
		}
		box, err := s.box.Seal(logging.Secret(plain), textsContext())
		if err != nil {
			return State{}, false, err
		}
		next.Texts = box
	}
	return next, true, nil
}

func (s *Settings) denied(ctx context.Context, req users.Request, action string) error {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: "forbidden", Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "settings", TargetID: "log_masking", Action: action, Result: audit.ResultDenied, Reason: "forbidden",
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
	return users.ErrForbidden
}
