// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
)

// TestSecretsNeverLeak: the RCON password of a server and a temporary
// password from a reset appear in no API answer except the reset itself,
// in no audit entry, no log line (debug, JSON) and no error message.
func TestSecretsNeverLeak(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin := f.login(t, "Boss")
	const origin = "http://panel.lan"
	const rconPW = "Rcon-Leak-Probe-9271"
	var answers []string
	call2 := func(c call) *httpRecorder {
		t.Helper()
		c.cookies = append(c.cookies, admin)
		if c.method != http.MethodGet {
			c.origin = origin
		}
		rec := f.do(t, c)
		answers = append(answers, rec.Body.String())
		return &httpRecorder{code: rec.Code, body: rec.Body.String()}
	}

	rec := call2(call{method: http.MethodGet, path: "/api/v1/profiles"})
	var profilesList api.ProfileList
	_ = json.Unmarshal([]byte(rec.body), &profilesList)
	profile := profilesList.Items[0].Id.String()

	rec = call2(call{method: http.MethodPost, path: "/api/v1/servers",
		body: `{"name":"mc","profile_ids":["` + profile + `"],"host":"mc.lan","password":"` + rconPW + `"}`})
	if rec.code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.code, rec.body)
	}
	var srv api.Server
	_ = json.Unmarshal([]byte(rec.body), &srv)
	id := srv.Id.String()
	if srv.ProfileIds == nil || len(*srv.ProfileIds) != 1 || (*srv.ProfileIds)[0].String() != profile {
		t.Errorf("profile_ids = %v", srv.ProfileIds)
	}
	// Several profiles follow with SRV-008; until then 422.
	if r := call2(call{method: http.MethodPost, path: "/api/v1/servers",
		body: `{"name":"two","profile_ids":["` + profile + `","` + profile + `"],"host":"mc.lan","password":"` + rconPW + `"}`}); r.code != http.StatusUnprocessableEntity {
		t.Errorf("two profiles = %d %s", r.code, r.body)
	}
	call2(call{method: http.MethodPatch, path: "/api/v1/servers/" + id, body: `{"password":"` + rconPW + `x"}`})
	// An invalid request with the password must not echo it.
	call2(call{method: http.MethodPost, path: "/api/v1/servers", body: `{"name":"x","host":"bad host","profile_ids":["` + profile + `"],"password":"` + rconPW + `"}`})
	call2(call{method: http.MethodPatch, path: "/api/v1/servers/" + id, body: `{"password":"` + rconPW + `","port":0}`})
	call2(call{method: http.MethodGet, path: "/api/v1/servers"})
	call2(call{method: http.MethodGet, path: "/api/v1/servers/" + id})

	reset := call2(call{method: http.MethodPost, path: "/api/v1/users/" + f.userID + "/password-reset"})
	var pr api.PasswordReset
	_ = json.Unmarshal([]byte(reset.body), &pr)
	if pr.Password == "" {
		t.Fatalf("reset = %d %s", reset.code, reset.body)
	}
	answers = answers[:len(answers)-1] // the reset answer is the one place the password belongs
	call2(call{method: http.MethodGet, path: "/api/v1/audit?limit=200"})
	call2(call{method: http.MethodGet, path: "/api/v1/users/" + f.userID})

	for _, secretValue := range []string{rconPW, pr.Password} {
		for i, a := range answers {
			if strings.Contains(a, secretValue) {
				t.Errorf("secret in API answer %d: %s", i, a)
			}
		}
		if strings.Contains(f.logs.String(), secretValue) {
			t.Error("secret in the log")
		}
	}
	if !strings.Contains(answers[len(answers)-2], `"server.create"`) {
		t.Error("audit answer does not contain the server entries; the check would be empty")
	}
}

type httpRecorder struct {
	code int
	body string
}
