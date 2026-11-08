// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentview"
	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
)

func (f *fixture) auditEntry(t *testing.T, id string, e audit.Entry) {
	t.Helper()
	e.ID = id
	if _, err := audit.Write(context.Background(), f.st.DB, e); err != nil {
		t.Fatal(err)
	}
}

// TestAuditScope covers AUD-004: the RCON admin sees everything, a server
// admin only entries of his scope; others cannot read the log.
func TestAuditScope(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	f.auditEntry(t, "e-s1", audit.Entry{Kind: audit.KindCommand, ServerID: "s1", Action: "cmd", Result: audit.ResultOK})
	f.auditEntry(t, "e-s2", audit.Entry{Kind: audit.KindCommand, ServerID: "s2", Action: "cmd", Result: audit.ResultOK})
	f.auditEntry(t, "e-m", audit.Entry{Kind: audit.KindAdmin, TargetType: "user", TargetID: f.ids["m"], Action: "x", Result: audit.ResultOK})
	f.auditEntry(t, "e-sa2", audit.Entry{Kind: audit.KindAdmin, TargetType: "user", TargetID: f.ids["sa2"], Action: "x", Result: audit.ResultOK})
	f.auditEntry(t, "e-own", audit.Entry{Kind: audit.KindAuth, ActorID: f.ids["sa"], Action: "login", Result: audit.ResultOK})

	ids := func(actor string, q AuditQuery) []string {
		t.Helper()
		es, _, err := f.svc.Audit(ctx, f.as(actor), q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range es {
			out = append(out, e.ID)
		}
		slices.Sort(out)
		return out
	}
	if got := ids("sa", AuditQuery{}); !slices.Equal(got, []string{"e-m", "e-own", "e-s1"}) {
		t.Errorf("server admin sees %v", got)
	}
	if got := ids("admin", AuditQuery{Kind: "command"}); !slices.Equal(got, []string{"e-s1", "e-s2"}) {
		t.Errorf("RCON admin commands %v", got)
	}
	if got := ids("admin", AuditQuery{ServerID: "s2"}); !slices.Equal(got, []string{"e-s2"}) {
		t.Errorf("filter by server %v", got)
	}
	if _, _, err := f.svc.Audit(ctx, f.as("m"), AuditQuery{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("moderator reads the audit: %v", err)
	}
}

func TestAuditPaging(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	for i := range 7 {
		f.auditEntry(t, fmt.Sprintf("p-%d", i), audit.Entry{Kind: audit.KindAuth, Action: "x", Result: audit.ResultOK})
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		es, next, err := f.svc.Audit(ctx, f.as("admin"), AuditQuery{Kind: "auth", Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, e := range es {
			if seen[e.ID] {
				t.Fatalf("%s twice", e.ID)
			}
			seen[e.ID] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 7 || pages != 3 {
		t.Errorf("seen %d entries on %d pages", len(seen), pages)
	}
	var inv *InvalidError
	if _, _, err := f.svc.Audit(ctx, f.as("admin"), AuditQuery{Cursor: "!!"}); !errors.As(err, &inv) {
		t.Errorf("bad cursor: %v", err)
	}
}

// TestSettingsOnlyRCONAdmin covers AUD-003 and AUD-004: only the RCON
// admin changes the retention; a server admin's attempt is refused and
// audited.
func TestSettingsOnlyRCONAdmin(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	if err := EnsureAuditDefaults(ctx, f.st); err != nil {
		t.Fatal(err)
	}
	in := map[string]json.RawMessage{SettingAuditRetentionDays: json.RawMessage("30")}
	if _, err := f.svc.UpdateSettings(ctx, f.as("sa"), in, f.version("settings", "")); !errors.Is(err, ErrForbidden) {
		t.Errorf("server admin changes the retention: %v", err)
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND result = 'denied'`) != 1 {
		t.Error("refused attempt not audited")
	}
	set, err := f.svc.UpdateSettings(ctx, f.as("admin"), in, f.version("settings", ""))
	if err != nil {
		t.Fatal(err)
	}
	if string(set[SettingAuditRetentionDays]) != "30" {
		t.Errorf("retention = %s", set[SettingAuditRetentionDays])
	}
	var inv *InvalidError
	for name, bad := range map[string]map[string]json.RawMessage{
		"unknown key":  {"evil.key": json.RawMessage("1")},
		"out of range": {SettingAuditRetentionDays: json.RawMessage("0")},
		"wrong type":   {"backup.interval": json.RawMessage("24")},
	} {
		if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), bad, f.version("settings", "")); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSupporterSwitchOffEndsSessions(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	on := map[string]json.RawMessage{perm.SettingSupportersEnabled: json.RawMessage("true")}
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), on, f.version("settings", "")); err != nil {
		t.Fatal(err)
	}
	f.revoked = nil
	off := map[string]json.RawMessage{perm.SettingSupportersEnabled: json.RawMessage("false")}
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), off, f.version("settings", "")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.revoked, []string{f.ids["sup"]}) {
		t.Errorf("sessions ended for %v, want all supporters", f.revoked)
	}
}

// TestAgentDataSettings covers the settings of AGT-006 and AGT-007: each
// takes its minimum and refuses anything below it.
func TestAgentDataSettings(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	var inv *InvalidError
	for key, low := range map[string]time.Duration{
		agentview.SettingVersionRetry:   agentview.MinVersionRetry,
		agentview.SettingVersionWindow:  agentview.MinVersionWindow,
		agentview.SettingVolumeInterval: agentview.MinVolumeInterval,
		agentview.SettingVolumeStale:    agentview.MinVolumeStale,
	} {
		ok := map[string]json.RawMessage{key: json.RawMessage(`"` + low.String() + `"`)}
		if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), ok, f.version("settings", "")); err != nil {
			t.Errorf("%s = %s: %v", key, low, err)
		}
		below := map[string]json.RawMessage{key: json.RawMessage(`"` + (low - time.Second).String() + `"`)}
		if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), below, f.version("settings", "")); !errors.As(err, &inv) {
			t.Errorf("%s below the minimum: %v", key, err)
		}
	}
}
