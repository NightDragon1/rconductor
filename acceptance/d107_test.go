// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// D-107: the built-in patterns black out the system log and
// the diagnostic package always, also with masking switched off. Two ways in
// over the public API: a server name in servers.json of the package, and a
// pseudonym in the event auth.invite.used of the system log.
func TestD107BuiltInPatternsWithMaskingOff(t *testing.T) {
	a := admin(t)
	const pwSecret = "hunter2abcAcct107"
	const bearerSecret = "abc123def456acct"

	// Masking off; on again at the end.
	setMasking(t, a, false)
	t.Cleanup(func() { setMasking(t, a, true) })

	// A server whose name carries a password form.
	var profiles struct {
		Items []struct {
			ID       string `json:"id"`
			Protocol string `json:"protocol"`
		} `json:"items"`
	}
	a.do(http.MethodGet, "/profiles", nil).want(t, "GET /profiles", http.StatusOK).decode(t, &profiles)
	profileID := ""
	for _, p := range profiles.Items {
		if p.Protocol == "source-rcon" {
			profileID = p.ID
			break
		}
	}
	if profileID == "" {
		t.Fatal("no profile with protocol source-rcon")
	}
	a.do(http.MethodPost, "/servers", map[string]any{
		"name": "password=" + pwSecret, "profile_ids": []string{profileID},
		"host": "127.0.0.1", "port": 27015, "password": "acct-rcon-107",
	}).want(t, "server with a password form in its name", http.StatusCreated)

	// A user whose pseudonym carries a bearer form (event auth.invite.used).
	since := time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339)
	invite(t, a, "supporter", "Bearer "+bearerSecret, "Acct-Supporter-107!")

	// Diagnostic package: no file holds the secrets.
	r := a.do(http.MethodGet, "/diagnostics", nil).want(t, "GET /diagnostics", http.StatusOK)
	zr, err := zip.NewReader(bytes.NewReader(r.body), int64(len(r.body)))
	if err != nil {
		t.Fatalf("package is no ZIP: %v", err)
	}
	sawServers := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if strings.Contains(f.Name, "servers") {
			sawServers = true
		}
		for _, s := range []string{pwSecret, bearerSecret} {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%s in the package holds a secret in clear text", f.Name)
			}
		}
	}
	if !sawServers {
		t.Error("package has no servers file; the test would prove nothing")
	}

	// System log of the panel: the bearer form is blacked out. The sink writes
	// about once a second, so the line is awaited for up to 15 s, read from the
	// time before the redeem on.
	sawInvite := false
	for i := 0; i < 15 && !sawInvite; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		r = a.do(http.MethodGet, "/system-log?source=panel&limit=200&from="+url.QueryEscape(since), nil).
			want(t, "GET /system-log", http.StatusOK)
		var page struct {
			Items []struct {
				Code string `json:"code"`
				Text string `json:"text"`
			} `json:"items"`
		}
		r.decode(t, &page)
		for _, e := range page.Items {
			if e.Code == "auth.invite.used" {
				sawInvite = true
			}
			if strings.Contains(e.Text, bearerSecret) {
				t.Fatalf("system log line %q holds the secret in clear text", e.Code)
			}
		}
	}
	if !sawInvite {
		t.Error("no line auth.invite.used in the system log within 15 s; the test would prove nothing")
	}
}

// setMasking switches the masking of logs on or off.
func setMasking(t *testing.T, a *client, on bool) {
	t.Helper()
	var m struct {
		Version string `json:"version"`
	}
	a.do(http.MethodGet, "/settings/log-masking", nil).want(t, "GET /settings/log-masking", http.StatusOK).decode(t, &m)
	a.do(http.MethodPut, "/settings/log-masking", map[string]any{"enabled": on}, "If-Match", `"`+m.Version+`"`).
		want(t, "PUT /settings/log-masking", http.StatusOK)
}
