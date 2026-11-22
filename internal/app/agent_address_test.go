// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentAddressOnlyRCONAdmin covers D-084 and AGT-014: the address an
// agent connected from reaches the RCON admin in the agent data and no
// other role in any answer about agents or servers.
func TestAgentAddressOnlyRCONAdmin(t *testing.T) {
	f := newRightsFixture(t)
	const addr = "203.0.113.7"
	agentID := f.agentRow("active")
	db, err := sql.Open("sqlite", filepath.Join(f.data, "rconductor.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), `UPDATE agents SET remote = ? WHERE id = ?`, addr, agentID); err != nil {
		t.Fatal(err)
	}
	f.as(rA).must(http.MethodPatch, "/servers/"+f.server, map[string]string{"agent_id": agentID, "container_name": "mc"}, nil, http.StatusOK)
	paths := []string{"/agents", "/agents/" + agentID, "/servers", "/servers/" + f.server, "/server-groups", "/overview", "/about"}
	for _, p := range paths[:2] {
		if status, body := f.rawBody(rA, http.MethodGet, p, nil); status != http.StatusOK || !strings.Contains(string(body), addr) {
			t.Errorf("RCON admin GET %s = %d, address missing", p, status)
		}
	}
	for _, role := range []string{rSI, rSO, rMR, rMN, rSU, rX} {
		for _, p := range paths {
			if status, body := f.rawBody(role, http.MethodGet, p, nil); strings.Contains(string(body), addr) {
				t.Errorf("%s GET %s = %d carries the agent address", role, p, status)
			}
		}
	}
}
