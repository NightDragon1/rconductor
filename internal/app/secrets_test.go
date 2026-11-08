// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// TestNoSecretsInTheLogs covers SEC-012: a run at debug level with panel,
// agent and game server, in which every kind of secret passes through,
// leaves none of them in the log of panel or agent. The initial password
// goes to the console, never to the log (AUTH-007).
func TestNoSecretsInTheLogs(t *testing.T) {
	const rconPassword = "the secret rcon password"
	game, err := rcontest.Start(rconPassword, rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	s := newStack(t, game.Addr, rconPassword)
	admin := s.users["admin"]
	secrets := []string{rconPassword, "the admin's own password", "password of sa1", "password of sa2", admin.cookie.Value}

	// A wrong password, an invitation and a new user.
	const wrong = "a wrong password somebody typed"
	secrets = append(secrets, wrong)
	anon := &client{t: t, base: s.base}
	anon.must(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "admin", "password": wrong}, nil, http.StatusUnauthorized)

	var inv struct{ Token string }
	admin.must(http.MethodPost, "/invites", map[string]string{"role": "moderator"}, &inv, http.StatusCreated)
	const invited = "the password of the invited user"
	var user struct{ ID string }
	anon.must(http.MethodPost, "/invites/redeem", map[string]string{"token": inv.Token, "pseudonym": "mod", "password": invited}, &user, http.StatusCreated)
	moderator := login(t, s.base, "mod", invited)
	secrets = append(secrets, inv.Token, invited, moderator.cookie.Value)
	// A login with the own session cookie sent along ends that session
	// (AUTH-006, D-052): neither cookie may reach the log.
	secrets = append(secrets, reloginWith(t, s.base, moderator.cookie, "mod", invited).Value)

	// A temporary password and a change of password.
	var reset struct{ Password string }
	admin.must(http.MethodPost, "/users/"+user.ID+"/password-reset", nil, &reset, http.StatusOK)
	const changed = "the changed password of the user"
	temporary := login(t, s.base, "mod", reset.Password)
	temporary.must(http.MethodPost, "/me/password", map[string]string{"current_password": reset.Password, "new_password": changed}, nil, http.StatusNoContent)
	secrets = append(secrets, reset.Password, changed)

	// The SMTP setup with a password and a test mail that fails (NFR-017):
	// neither the password nor the recipient's name may reach the log.
	const smtpPassword = "the secret smtp password"
	admin.must(http.MethodPut, "/settings/smtp", map[string]any{"host": "127.0.0.1", "port": 1, "encryption": "ssl",
		"username": "mailer", "password": smtpPassword, "from": "panel@example.org"}, nil, http.StatusOK)
	var tested struct{ Ok bool }
	admin.must(http.MethodPost, "/settings/smtp/test", map[string]string{"to": "hidden-recipient@example.org"}, &tested, http.StatusOK)
	if tested.Ok {
		t.Error("test mail to a closed port went out")
	}
	logtest.Wait(t, s.logs, `"event":"mail.failed"`)
	secrets = append(secrets, smtpPassword, "hidden-recipient")
	var smtp map[string]any
	admin.must(http.MethodGet, "/settings/smtp", nil, &smtp, http.StatusOK)
	if b, _ := json.Marshal(smtp); strings.Contains(string(b), smtpPassword) || smtp["password_set"] != true {
		t.Errorf("GET /settings/smtp = %s", b)
	}

	// A second factor: secret and the cookie of the second step never
	// reach the log (AUTH-008).
	sa := s.users["sa2"]
	var setup struct{ Secret string }
	sa.must(http.MethodPost, "/me/second-factor/setup", map[string]string{"password": "password of sa2"}, &setup, http.StatusOK)
	first := codeAt(t, setup.Secret, 0)
	sa.must(http.MethodPost, "/me/second-factor/confirm", map[string]string{"code": first}, nil, http.StatusNoContent)
	_, _, cookies := post(t, s.base, "/auth/login", map[string]string{"pseudonym": "sa2", "password": "password of sa2"})
	pending := cookieNamed(cookies, "rcd_pending")
	if pending == nil {
		t.Fatal("no second step")
	}
	second := codeAt(t, setup.Secret, 1)
	_, _, cookies = post(t, s.base, "/auth/login/totp", map[string]string{"code": second}, pending)
	if c := cookieNamed(cookies, "rcd_session"); c != nil {
		secrets = append(secrets, c.Value)
	}
	// The codes themselves are six digits that a timestamp may hold by
	// chance; the secret they come from is checked.
	secrets = append(secrets, setup.Secret, pending.Value)

	// A command and the pairing of an agent.
	if _, code := s.send("admin", "list"); code != http.StatusOK {
		t.Errorf("command = %d", code)
	}
	var tok struct{ Token string }
	admin.must(http.MethodPost, "/agents/tokens", nil, &tok, http.StatusCreated)
	secrets = append(secrets, tok.Token)
	cfg := config.Agent{
		PairingToken: tok.Token, PanelAddr: s.agentAddr, SecretsDir: t.TempDir(), Name: "host-1",
		LabelKey: "rconductor.manage", LabelValue: "true", Log: config.Log{Level: -4, Format: "json"},
	}
	var agentLogs syncBuffer
	a, err := agent.New(cfg, logging.New(&agentLogs, "agent", cfg.Log).Root(), &hostBackend{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	logtest.Wait(t, &agentLogs, `"event":"agent.handshake"`)
	logtest.Wait(t, s.logs, `"event":"agent.connected"`, `"event":"auth.login.failed"`, `"event":"cmd.response"`, `"event":"auth.session.revoked"`)
	time.Sleep(200 * time.Millisecond) // lines that follow the last event
	stop()
	if err := <-done; err != nil {
		t.Errorf("agent: %v", err)
	}

	for name, logs := range map[string]string{"panel": s.logs.String(), "agent": agentLogs.String()} {
		if !strings.Contains(logs, `"level":"DEBUG"`) && !strings.Contains(logs, `"level":"debug"`) && name == "panel" {
			t.Errorf("the %s log has no debug lines, so the run proves little", name)
		}
		for _, secret := range secrets {
			if strings.Contains(logs, secret) {
				t.Errorf("%s log holds a secret of %d characters", name, len(secret))
			}
		}
	}
}

// reloginWith logs in while sending cookie along, as a browser with an
// open session does, and returns the new session cookie.
func reloginWith(t *testing.T, base string, cookie *http.Cookie, pseudonym, password string) *http.Cookie {
	t.Helper()
	body := strings.NewReader(`{"pseudonym":"` + pseudonym + `","password":"` + password + `"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/api/v1/auth/login", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with cookie sent along = %d", resp.StatusCode)
	}
	var fresh *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookie.Name && c.Value != "" {
			fresh = c
		}
	}
	if fresh == nil || fresh.Value == cookie.Value {
		t.Fatalf("login with cookie sent along gave no new session: %v", resp.Header.Values("Set-Cookie"))
	}
	return fresh
}
