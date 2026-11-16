// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestLogMaskingUI022 covers UI-022 with a real agent: known secrets in
// container logs are masked in the panel before cache and delivery, so the
// plain value reaches no WebSocket topic and no log tail; with the switch
// off the lines come as they are. Switch and patterns are the RCON admin's
// alone (403), an invalid pattern is 400, and each change is audited.
func TestLogMaskingUI022(t *testing.T) {
	f := newRightsFixture(t)
	const secret = "hunter2x"
	backend := &hostBackend{logLine: "java --rcon-password " + secret + " RCON_PASSWORD=" + secret + " steam_4711"}
	pairAgent(t, f.as(rA), f.agentAddr, f.server, backend)
	rec := recordTopics(t, f, rA, f.server)
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(rec.all(), "--rcon-password •••") {
		if time.Now().After(deadline) {
			t.Fatalf("no masked log line over the WebSocket:\n%s", rec.all())
		}
		time.Sleep(20 * time.Millisecond)
	}
	tail := func(role string) string {
		t.Helper()
		status, body := f.rawBody(role, http.MethodGet, "/servers/"+f.server+"/logs?lines=5", nil)
		if status != http.StatusOK {
			t.Fatalf("%s tail = %d %s", role, status, body)
		}
		return string(body)
	}
	for _, role := range []string{rA, rSI} {
		if got := tail(role); strings.Contains(got, secret) || !strings.Contains(got, "RCON_PASSWORD=•••") {
			t.Errorf("%s tail = %s", role, got)
		}
	}

	// Patterns and switch: RCON admin only, invalid patterns 400.
	if got := f.as(rSI).call(http.MethodPatch, "/settings", map[string]any{"logs.mask_patterns": []string{"steam_[0-9]+"}}, nil); got != http.StatusForbidden {
		t.Errorf("server admin sets patterns = %d, want 403", got)
	}
	for _, bad := range []any{[]string{"("}, []string{strings.Repeat("a", 257)}, make([]string, 21), "steam"} {
		if got := f.as(rA).call(http.MethodPatch, "/settings", map[string]any{"logs.mask_patterns": bad}, nil); got != http.StatusBadRequest {
			t.Errorf("patterns %v = %d, want 400", bad, got)
		}
	}
	f.as(rA).must(http.MethodPatch, "/settings", map[string]any{"logs.mask_patterns": []string{"steam_[0-9]+"}}, nil, http.StatusOK)
	if got := tail(rA); strings.Contains(got, "steam_4711") {
		t.Errorf("admin pattern not applied: %s", got)
	}
	f.as(rA).must(http.MethodPatch, "/settings", map[string]any{"logs.masking": false}, nil, http.StatusOK)
	if got := tail(rA); !strings.Contains(got, "--rcon-password "+secret) {
		t.Errorf("switched off: %s", got)
	}
	var n int
	if err := f.db().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'settings.update' AND result = 'ok' AND payload LIKE '%logs.mask%'`).Scan(&n); err != nil || n < 2 {
		t.Errorf("audit entries of the masking = %d %v, want 2", n, err)
	}
}
