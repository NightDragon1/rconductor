// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

const secretText = "s3cr3t-value"

func newBox(t *testing.T) *secret.Box {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	b, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newSettings(t *testing.T) (*Settings, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	return NewSettings(storetest.New(t), newBox(t), slog.New(slog.NewJSONHandler(&logs, nil))), &logs
}

func settingsRequest(t *testing.T, s *Settings, role perm.Role) users.Request {
	t.Helper()
	stored := role
	if role != perm.RCONAdmin && role != perm.ServerAdmin && role != perm.Moderator && role != perm.Supporter {
		stored = perm.Supporter // unknown role of a session, on a stored user
	}
	id := storetest.CreateUser(t, s.st, storetest.User{Pseudonym: "u" + string(role), Role: string(stored)})
	return users.Request{Actor: perm.Actor{ID: id, Role: role}, Pseudonym: string(role)}
}

func version(v int64) *int64 { return &v }
func num(v int) *int         { return &v }

// TestSettingsRoles: only the RCON admin passes (UI-022); refusals audited.
func TestSettingsRoles(t *testing.T) {
	s, _ := newSettings(t)
	ctx := context.Background()
	for _, role := range []perm.Role{perm.ServerAdmin, perm.Moderator, perm.Supporter, "", "unknown"} {
		req := settingsRequest(t, s, role)
		if _, err := s.Read(ctx, req); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%q get: %v", role, err)
		}
		if _, err := s.Write(ctx, req, SettingsInput{SetTexts: true, Texts: []Text{{Value: "x"}}}); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%q update: %v", role, err)
		}
	}
	var n int
	if err := s.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE result = 'denied' AND target_id = 'log_masking'`).Scan(&n); err != nil || n != 10 {
		t.Errorf("refusals = %d (%v), want 10", n, err)
	}
}

// TestSettingsFlow: defaults, sealed texts, audit without a text, {} keeps
// the version, 409, limits against stored texts; no text in row, audit, log.
func TestSettingsFlow(t *testing.T) {
	s, logs := newSettings(t)
	ctx := context.Background()
	admin := settingsRequest(t, s, perm.RCONAdmin)
	if st, err := s.Read(ctx, admin); err != nil || st.Version != 0 || !st.Enabled || st.MaxTexts != 50 || st.TextRunes != 256 {
		t.Fatalf("defaults: %+v %v", st, err)
	}
	if _, err := s.Write(ctx, admin, SettingsInput{}); !errors.Is(err, users.ErrPreconditionRequired) {
		t.Errorf("no version: %v", err)
	}
	off := false
	st, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), Enabled: &off, SetTexts: true, MaxTexts: num(10),
		Texts: []Text{{Value: secretText}, {Value: "OTHER-" + secretText, Fold: true}}})
	if err != nil || st.Version != 1 || st.Enabled || st.TextCount != 2 || st.MaxTexts != 10 {
		t.Fatalf("change: %+v %v", st, err)
	}
	if texts, err := s.OpenTexts(st); err != nil || len(texts) != 2 || texts[1].Value != "OTHER-"+secretText || !texts[1].Fold {
		t.Errorf("open: %v %v", texts, err)
	}
	if again, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(1)}); err != nil || again.Version != 1 {
		t.Errorf("{}: %+v %v", again, err)
	}
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), MaxTexts: num(20)}); !errors.Is(err, users.ErrConflict) {
		t.Errorf("stale: %v", err)
	}
	// The stored texts must fit a new limit, checked before the version.
	var inv *users.InvalidError
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), TextRunes: num(8)}); !errors.As(err, &inv) {
		t.Errorf("limit under the stored texts: %v", err)
	}
	if _, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(1), SetTexts: true, Texts: []Text{}}); err != nil {
		t.Errorf("remove texts: %v", err)
	}
	var row, auditRows string
	_ = s.st.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, users.SettingLogMasking).Scan(&row)
	_ = s.st.DB.QueryRow(`SELECT group_concat(payload, '|') FROM audit_log WHERE action = 'log_masking.updated'`).Scan(&auditRows)
	if strings.Count(auditRows, `"texts_replaced":true`) != 2 || !strings.Contains(auditRows, `"text_count":{"from":0,"to":2}`) {
		t.Errorf("audit: %s", auditRows)
	}
	for name, v := range map[string]string{"row": row, "audit": auditRows, "log": logs.String()} {
		if strings.Contains(v, "s3cr3t") {
			t.Errorf("%s holds a text: %s", name, v)
		}
	}
}

// TestSettingsInput: misuse is 400 and names a position, never a text.
func TestSettingsInput(t *testing.T) {
	s, _ := newSettings(t)
	admin := settingsRequest(t, s, perm.RCONAdmin)
	many := make([]Text, MaxTexts+1)
	for i := range many {
		many[i] = Text{Value: secretText}
	}
	for name, in := range map[string]SettingsInput{
		"too many":    {SetTexts: true, Texts: many, MaxTexts: num(MaxTexts)},
		"too long":    {SetTexts: true, Texts: []Text{{Value: strings.Repeat("s3cr3t", 200)}}, TextRunes: num(MaxTextRunes)},
		"empty value": {SetTexts: true, Texts: []Text{{}}},
		"CR":          {SetTexts: true, Texts: []Text{{Value: secretText + "\r"}}},
		"max_texts 0": {MaxTexts: num(0)},
	} {
		in.IfMatch = version(0)
		_, err := s.Write(context.Background(), admin, in)
		var inv *users.InvalidError
		if !errors.As(err, &inv) || strings.Contains(inv.Detail, "s3c") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestSettingsUnreadable: another master key or a broken row (F-93).
func TestSettingsUnreadable(t *testing.T) {
	s, logs := newSettings(t)
	ctx := context.Background()
	admin := settingsRequest(t, s, perm.RCONAdmin)
	st, err := s.Write(ctx, admin, SettingsInput{IfMatch: version(0), SetTexts: true, Texts: []Text{{Value: secretText}}})
	if err != nil {
		t.Fatal(err)
	}
	other := NewSettings(s.st, newBox(t), nil)
	if _, err := other.OpenTexts(st); !errors.Is(err, ErrUnreadable) {
		t.Errorf("other key: %v", err)
	}
	if _, err := s.st.DB.Exec(`UPDATE settings SET value = '{"version":1,"max_texts":0}' WHERE key = ?`, users.SettingLogMasking); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, admin); !errors.Is(err, ErrUnreadable) || !strings.Contains(logs.String(), "setting.read_failed") {
		t.Errorf("broken row: %v, log %s", err, logs.String())
	}
}

// TestSettingsParallel: of parallel writes on one version exactly one passes.
func TestSettingsParallel(t *testing.T) {
	s, _ := newSettings(t)
	admin := settingsRequest(t, s, perm.RCONAdmin)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, conflict := 0, 0
	for i := range 16 {
		wg.Go(func() {
			_, err := s.Write(context.Background(), admin, SettingsInput{IfMatch: version(0), MaxTexts: num(1 + i)})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, users.ErrConflict):
				conflict++
			default:
				t.Errorf("write: %v", err)
			}
		})
	}
	wg.Wait()
	if ok != 1 || conflict != 15 {
		t.Errorf("ok %d, conflict %d", ok, conflict)
	}
}
