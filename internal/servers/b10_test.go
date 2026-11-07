// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/NightDragon1/rconductor/internal/users"
)

type updatePayload struct {
	Fields []string
	Host   *struct{ From, To string }
	Port   *struct{ From, To int }
}

func lastUpdate(t *testing.T, f *fixture, id string) (updatePayload, int) {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action = 'server.update' AND target_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	var p updatePayload
	if n == 0 {
		return p, 0
	}
	var payload string
	if err := f.st.DB.QueryRowContext(ctx,
		`SELECT payload FROM audit_log WHERE action = 'server.update' AND target_id = ? ORDER BY rowid DESC LIMIT 1`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatal(err)
	}
	return p, n
}

// TestB10_1AuditNamesSentNotChangedFields: the UI sends the whole form on
// every save. A rename must audit only the name, so that a move of the
// address stays visible; a move names the old and the new host and port.
func TestB10_1AuditNamesSentNotChangedFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sv := f.create("alpha")
	name, host, port, profile := "beta", sv.Host, sv.Port, sv.ProfileID
	agent, container := "", ""
	if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{
		Name: &name, ProfileID: &profile, Host: &host, Port: &port,
		AgentID: &agent, ContainerName: &container, Version: f.version(sv.ID),
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := lastUpdate(t, f, sv.ID); !slices.Equal(got.Fields, []string{"name"}) || got.Host != nil || got.Port != nil {
		t.Errorf("rename audited as %+v", got)
	}
	newHost, newPort := "elsewhere.example", 27015
	if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{
		Name: &name, ProfileID: &profile, Host: &newHost, Port: &newPort,
		AgentID: &agent, ContainerName: &container, Version: f.version(sv.ID),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := lastUpdate(t, f, sv.ID)
	if !slices.Equal(got.Fields, []string{"host", "port"}) || got.Host == nil || got.Host.From != sv.Host || got.Host.To != newHost ||
		got.Port == nil || got.Port.From != sv.Port || got.Port.To != newPort {
		t.Errorf("move audited as %+v", got)
	}
	pw := secretOf("a new rcon password")
	if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{Password: pw, Version: f.version(sv.ID)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := lastUpdate(t, f, sv.ID); !slices.Equal(got.Fields, []string{"password"}) {
		t.Errorf("new password audited as %+v", got)
	}
}

// TestB10_2ProfileChangeMovesPortUnaudited: a profile change without port
// moves the port to the profile's default; the audit must name the port with
// both values.
func TestB10_2ProfileChangeMovesPortUnaudited(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sv := f.create("alpha")
	f.exec(`UPDATE game_profiles SET default_port = 4444 WHERE slug = 'minecraft-gtnh'`)
	var other string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'minecraft-gtnh'`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{ProfileID: &other, Version: f.version(sv.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Port == sv.Port {
		t.Fatalf("port did not move: %d", got.Port)
	}
	p, _ := lastUpdate(t, f, sv.ID)
	if !slices.Equal(p.Fields, []string{"port", "profile_id"}) || p.Port == nil || p.Port.From != sv.Port || p.Port.To != 4444 {
		t.Errorf("port moved %d -> %d, audit = %+v", sv.Port, got.Port, p)
	}
}

// TestUpdateWithoutChange: a save that changes nothing writes no audit
// entry and no new version; a stale version is still a conflict.
func TestUpdateWithoutChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sv := f.create("alpha")
	before := *f.version(sv.ID)
	name, host, port := sv.Name, sv.Host, sv.Port
	for range 2 {
		if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{Name: &name, Host: &host, Port: &port, Version: &before}); err != nil {
			t.Fatal(err)
		}
	}
	if _, n := lastUpdate(t, f, sv.ID); n != 0 {
		t.Errorf("%d audit entries for saves without change", n)
	}
	if v := *f.version(sv.ID); v != before {
		t.Errorf("version %d after saves without change, was %d", v, before)
	}
	stale := before - 1
	if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{Name: &name, Version: &stale}); !errors.Is(err, users.ErrConflict) {
		t.Errorf("stale version without change: %v", err)
	}
	if _, err := f.svc.Update(ctx, f.as("m"), sv.ID, Input{Name: &name, Version: &before}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("moderator without change: %v", err)
	}
}
