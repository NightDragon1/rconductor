// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// The instance runs and asks for a session.
func TestProbeSessionNeedsSignIn(t *testing.T) {
	resp, err := http.Get(os.Getenv("RCD_ACCEPTANCE_URL") + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("GET /auth/session = %d %s, want 401 problem+json", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}
