// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/features"
)

// flagFixture guards the server list with a test flag (NFR-019).
func flagFixture(t *testing.T) (f *apiFixture, get func(*http.Cookie) int, patch func(*http.Cookie, string) int, on func(*http.Cookie) []string) {
	t.Helper()
	flags, err := features.New(features.Flag{Name: "test_flag", Operations: []string{"ListServers"}})
	if err != nil {
		t.Fatal(err)
	}
	f = newAPIFixtureFeatures(t, Proxies{}, flags)
	get = func(c *http.Cookie) int {
		return f.do(t, call{method: http.MethodGet, path: "/api/v1/servers", cookies: []*http.Cookie{c}}).Code
	}
	patch = func(c *http.Cookie, body string) int {
		return f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{c}, origin: "http://panel.lan", body: body}).Code
	}
	on = func(c *http.Cookie) []string {
		var s api.Session
		_ = json.Unmarshal(f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{c}}).Body.Bytes(), &s)
		if s.Ui.Features == nil {
			return nil
		}
		return *s.Ui.Features
	}
	return f, get, patch, on
}

func TestFeatureFlagOffIsNotFound(t *testing.T) {
	f, get, patch, on := flagFixture(t)
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	if got := f.do(t, call{method: http.MethodGet, path: "/api/v1/servers"}).Code; got != http.StatusUnauthorized {
		t.Errorf("without session = %d, want 401 as for every operation", got)
	}
	off := f.do(t, call{method: http.MethodGet, path: "/api/v1/servers", cookies: []*http.Cookie{admin}})
	unknown := f.do(t, call{method: http.MethodGet, path: "/api/v1/no-such-endpoint", cookies: []*http.Cookie{admin}})
	if off.Code != http.StatusNotFound || unknown.Code != off.Code || strings.Contains(off.Body.String(), "test_flag") {
		t.Errorf("flag off = %d %s, unknown endpoint = %d", off.Code, off.Body.String(), unknown.Code)
	}
	if patch(mod, `{"features.test_flag":true}`) != http.StatusForbidden || get(admin) != http.StatusNotFound || len(on(mod)) != 0 {
		t.Error("a moderator switched the flag")
	}
	if got := patch(admin, `{"features.test_flag":true}`); got != http.StatusOK {
		t.Fatalf("admin switches the flag: %d", got)
	}
	if got := on(mod); get(mod) != http.StatusOK || len(got) != 1 || got[0] != "test_flag" {
		t.Errorf("flag on: endpoint %d, session %v", get(mod), got)
	}
	audited := 0
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'settings.update' AND result = 'ok'
		AND payload LIKE '%features.test_flag%'`).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("audit entries of the switch = %d (%v), want 1", audited, err)
	}
	if patch(admin, `{"features.test_flag":false}`) != http.StatusOK || get(admin) != http.StatusNotFound {
		t.Error("switching the flag off again")
	}
}

func TestFeatureFlagSettingsAbuse(t *testing.T) {
	f, get, patch, on := flagFixture(t)
	admin := f.login(t, "Boss")
	many := make([]string, 10000)
	for i := range many {
		many[i] = fmt.Sprintf(`"features.f%d":true`, i)
	}
	for _, body := range []string{`{"features.other":true}`, `{"features.":true}`, `{"features.test_flag":"yes"}`,
		`{"features.test_flag":1}`, `{"features.test_flag":null}`, "{" + strings.Join(many, ",") + "}"} {
		if got := patch(admin, body); got < 400 || got >= 500 {
			t.Errorf("%.40s: %d, want a 4xx refusal", body, got)
		}
	}
	if got := on(admin); len(got) != 0 {
		t.Errorf("refused changes switched a flag: %v", got)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if got := patch(admin, fmt.Sprintf(`{"features.test_flag":%t}`, i%2 == 0)); got != http.StatusOK && got != http.StatusConflict {
				t.Errorf("parallel switch: %d", got)
			}
		})
	}
	wg.Wait()
	if (len(on(admin)) == 1) != (get(admin) == http.StatusOK) {
		t.Errorf("session says %v, endpoint answers %d", on(admin), get(admin))
	}
}
