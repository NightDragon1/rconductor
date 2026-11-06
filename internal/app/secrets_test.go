// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
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

	// A temporary password and a change of password.
	var reset struct{ Password string }
	admin.must(http.MethodPost, "/users/"+user.ID+"/password-reset", nil, &reset, http.StatusOK)
	const changed = "the changed password of the user"
	temporary := login(t, s.base, "mod", reset.Password)
	temporary.must(http.MethodPost, "/me/password", map[string]string{"current_password": reset.Password, "new_password": changed}, nil, http.StatusNoContent)
	secrets = append(secrets, reset.Password, changed)

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
	logtest.Wait(t, s.logs, `"event":"agent.connected"`, `"event":"auth.login.failed"`, `"event":"cmd.response"`)
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
