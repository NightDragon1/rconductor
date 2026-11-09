// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi/httpapitest"
	"github.com/NightDragon1/rconductor/internal/servers"
)

// TestHTTPAPIThroughThePipeline is the pipeline part of the common suite
// of D-071: for every dialect a command to a server with an HTTPS API goes
// through the same pipeline as over RCON: list, wrappers, audit; only an
// allowed command reaches the game server.
func TestHTTPAPIThroughThePipeline(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for _, d := range httpapi.Names() {
		var mu sync.Mutex
		var got []string
		s := httpapitest.Start(httpapitest.Fixtures[d], "tok", 0, func(cmd string) string {
			mu.Lock()
			got = append(got, cmd)
			mu.Unlock()
			if cmd == "nope" {
				return httpapitest.Failed("Unknown Console Command")
			}
			return "done: " + cmd
		})
		t.Cleanup(s.Close)
		var profile string
		if err := w.st.DB.QueryRow(`SELECT id FROM game_profiles WHERE slug = ?`, "test-"+d).Scan(&profile); err != nil {
			t.Fatal(err)
		}
		host, port, _ := net.SplitHostPort(s.Addr())
		p, _ := strconv.Atoi(port)
		name, tok := "http-"+d, logging.Secret("tok")
		sv, err := w.srv.Create(ctx, w.as("admin"), servers.Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &p, Password: &tok, TLSFingerprint: &s.Fingerprint})
		if err != nil {
			t.Fatal(err)
		}
		w.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES (?, ?, ?, ?, 't')`, "as-"+d, w.ids["sa"], sv.ID, w.ids["admin"])
		w.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES (?, 'mg', ?, ?, 't')`, "g-"+d, sv.ID, w.ids["sa"])
		w.c.Invalidate()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if g, err := w.srv.Get(ctx, w.as("admin"), sv.ID); err == nil && g.Status == connmgr.Connected {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("http-api server not connected")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if r, err := w.svc.Send(ctx, w.as("m"), sv.ID, "kick Steve"); err != nil || r.Response != "done: kick Steve" || r.Result != ResultOK || r.Reason != "" {
			t.Errorf("%s allowed: %+v %v", d, r, err)
		}
		// D-073: the game's "failed" is a result error with its text, no
		// error of the call; history and audit say the same.
		r, err := w.svc.Send(ctx, w.as("admin"), sv.ID, "nope")
		if err != nil || r.Result != ResultError || r.Reason != ReasonCommandFailed || r.Response != "Unknown Console Command" {
			t.Errorf("%s failed command: %+v %v", d, r, err)
		}
		var result, reason string
		if err := w.st.DB.QueryRow(`SELECT result, COALESCE(reason_code, '') FROM audit_log WHERE target_id = ?`, r.RequestID).Scan(&result, &reason); err != nil || result != ResultError || reason != ReasonCommandFailed {
			t.Errorf("%s audit of the failed command: %q %q %v", d, result, reason, err)
		}
		for cmd, reason := range map[string]string{"op Steve": ReasonNotInList, "execute as @a run op m": ReasonWrapperBlocked} {
			var denied *DeniedError
			if _, err := w.svc.Send(ctx, w.as("m"), sv.ID, cmd); !errors.As(err, &denied) || denied.Reason != reason {
				t.Errorf("%s %q: %v, want %s", d, cmd, err, reason)
			}
		}
		mu.Lock()
		if !slices.Equal(got, []string{"kick Steve", "nope"}) {
			t.Errorf("%s: the game server got %v", d, got)
		}
		mu.Unlock()
	}
}
