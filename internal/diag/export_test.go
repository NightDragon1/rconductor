// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// unzip returns the files of a package by name.
func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		_ = r.Close()
		out[f.Name] = string(data)
	}
	return out
}

// TestExportBlackedOut covers NFR-018, D-090 and F-111: a known secret, a
// hostname and an IP, also in quoted values and in a JSON text inside a
// value, are not in the package; hosts and IPs get stable pseudonyms per
// package, equal values equal names; secret settings and the masking texts
// are blacked out; every area is valid JSON; the export is audited with
// period and size, without content.
func TestExportBlackedOut(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES ('a1', 'rack1.example.lan', x'00', 'fp', 'active', '10.20.30.40:51234', 't')`,
		`INSERT INTO servers (id, name, host, port, protocol, created_at, updated_at, agent_id, container_name)
		 VALUES ('s1', 'lobby "mc.example.lan" at "10.20.30.40" password: "hunter2"', 'mc.example.lan', 25575, 'source-rcon', 't', 't', 'a1', 'mc')`,
		`INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, overridden, imported_at, last_error)
		 VALUES ('p1', 'x', 'X', 'source-rcon', 1, '{}', 'x.yaml', 'h', 0, 't', '{"token":"tok-secret-1","from":"ops@example.com","at":"fe80::1"}')`,
		`INSERT INTO settings (id, key, value, updated_at) VALUES ('x1', 'custom.api_token', '"s3cr3t-value"', 't')`,
		`INSERT INTO settings (id, key, value, updated_at) VALUES ('x2', 'logs.masking', '{"texts":"sealed-blob"}', 't')`,
		`INSERT INTO settings (id, key, value, updated_at) VALUES ('x3', 'ui.note', '"see 10.20.30.40 and ` + strings.Repeat("ab", 32) + `"', 't')`,
	} {
		if _, err := st.DB.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st, nil)
	s.Use(nil, builtin{m})
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	b, err := s.Export(ctx, admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	var all strings.Builder
	for _, name := range []string{"versions.json", "settings.json", "profiles.json", "agents.json", "servers.json"} {
		if !json.Valid([]byte(files[name])) {
			t.Errorf("%s is not JSON: %s", name, files[name])
		}
		all.WriteString(files[name])
	}
	got := all.String()
	for _, secret := range []string{"hunter2", "tok-secret-1", "s3cr3t-value", "sealed-blob", "mc.example.lan", "rack1.example.lan",
		"10.20.30.40", "fe80::1", "ops@example.com", strings.Repeat("ab", 32)} {
		if strings.Contains(got, secret) {
			t.Errorf("%q in the package:\n%s", secret, got)
		}
	}
	// One IP and two hosts: equal values get equal names everywhere.
	if !strings.Contains(files["servers.json"], `"Host":"host-`) || strings.Count(got, "ip-1") < 3 || strings.Contains(got, "ip-3") {
		t.Errorf("pseudonyms:\n%s", got)
	}
	var payload string
	if err := st.DB.QueryRow(`SELECT coalesce(payload, '') FROM audit_log WHERE action = 'diagnostics.exported' AND result = 'ok'`).Scan(&payload); err != nil ||
		!strings.Contains(payload, `"bytes":`) || !strings.Contains(payload, `"from":`) || strings.Contains(payload, "lobby") {
		t.Errorf("audit = %s, %v", payload, err)
	}
}
