// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
)

// TestAgentsAPI: only the RCON admin lists agents and creates pairing
// tokens; the token answer carries what the pairing dialog needs (AGT-005).
func TestAgentsAPI(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	mod := f.login(t, "Mod")

	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/agents", cookies: []*http.Cookie{admin}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("list = %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(t, call{method: http.MethodPost, path: "/api/v1/agents/tokens", cookies: []*http.Cookie{admin}, origin: "http://panel.lan"})
	var tok api.AgentToken
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("token = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(tok.Token, "rcd1.") || tok.PanelAddr == nil || *tok.PanelAddr != "panel.lan:8443" ||
		!strings.HasPrefix(tok.PanelFingerprint, "SHA256:") || !strings.Contains(tok.Compose, tok.Token) {
		t.Errorf("token = %+v", tok)
	}
	if strings.Contains(f.logs.String(), tok.Token) {
		t.Error("pairing token in the log")
	}
	for _, c := range []call{
		{method: http.MethodGet, path: "/api/v1/agents"},
		{method: http.MethodPost, path: "/api/v1/agents/tokens", origin: "http://panel.lan"},
	} {
		c.cookies = []*http.Cookie{mod}
		if rec := f.do(t, c); rec.Code != http.StatusForbidden {
			t.Errorf("moderator %s %s = %d", c.method, c.path, rec.Code)
		}
	}
	rec = f.do(t, call{method: http.MethodDelete, path: "/api/v1/agents/01a19e38-161d-712a-a6d1-f8eca4a6aacd", cookies: []*http.Cookie{admin}, origin: "http://panel.lan"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown = %d", rec.Code)
	}
}
