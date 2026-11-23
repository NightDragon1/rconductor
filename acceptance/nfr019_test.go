// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

import (
	"net/http"
	"strings"
	"testing"
)

// NFR-019 (1.3): the feature flags of 1.2 are gone. The settings know no
// key features.*, a write of one is refused as an unknown key, and the
// session has no ui.features. The functions behind the old flags answer.
func TestNFR019NoFeatureFlags(t *testing.T) {
	a := admin(t)

	s, etag := settings(t, a)
	for k := range s {
		if strings.HasPrefix(k, "features.") {
			t.Errorf("setting %q still exists", k)
		}
	}

	r := a.do(http.MethodPatch, "/settings", map[string]any{"features.messages": true}, "If-Match", etag)
	if r.status != http.StatusBadRequest && r.status != http.StatusUnprocessableEntity {
		t.Errorf("PATCH features.messages: status %d, want 400 or 422 (unknown key); body %s", r.status, trim(r.body))
	}

	var sess struct {
		UI map[string]any `json:"ui"`
	}
	a.do(http.MethodGet, "/auth/session", nil).want(t, "GET /auth/session", http.StatusOK).decode(t, &sess)
	if _, ok := sess.UI["features"]; ok {
		t.Error("session still has ui.features")
	}

	// Functions of former flags answer for the admin.
	for _, p := range []string{"/messages", "/system-log"} {
		r := a.do(http.MethodGet, p, nil)
		if r.status != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200; body %s", p, r.status, trim(r.body))
		}
	}
}
