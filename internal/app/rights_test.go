// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/goccy/go-yaml"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// The roles of the rights matrix, in the order of the expectation strings.
const (
	rA  = "admin"     // RCON admin
	rSI = "sa_in"     // server admin with the server in scope
	rSO = "sa_out"    // server admin with another server only
	rMR = "mod_rel"   // moderator with supporter release (global and own)
	rMN = "mod_norel" // moderator without own release
	rSU = "sup"       // supporter of mod_rel on the server
	rX  = "anon"      // no session
	rP  = "pwchange"  // must change the password (AUTH-010)
)

var matrixRoles = []string{rA, rSI, rSO, rMR, rMN, rSU, rX, rP}

const matrixPassword = "matrix test password"

// rightsFixture is a running panel with every role of the matrix.
type rightsFixture struct {
	t               *testing.T
	base, data      string
	c               map[string]*client
	ids             map[string]string // user IDs by role
	server, server2 string
	serverGroup     string
	group, list     string
	pwchangeTemp    string
	freshName       int
	tmpl            string
	// agentAddr is the panel's agent port.
	agentAddr string
}

func (f *rightsFixture) unique(prefix string) string {
	f.freshName++
	return fmt.Sprintf("%s-%d", prefix, f.freshName)
}

// raw sends a request as role and returns status and reason code.
func (f *rightsFixture) raw(role, method, path string, body any) (int, string) {
	f.t.Helper()
	status, b := f.rawBody(role, method, path, body)
	var p struct{ Reason string }
	_ = json.Unmarshal(b, &p)
	return status, p.Reason
}

// rawBody sends a request as role and returns status and body.
func (f *rightsFixture) rawBody(role, method, path string, body any) (int, []byte) {
	f.t.Helper()
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ctype = bytes.NewReader(b), "image/png"
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.base+"/api/v1"+path, rd)
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Origin", f.base)
	if c := f.c[role]; c != nil && c.cookie != nil {
		req.AddCookie(c.cookie)
		// Versioned writes name the version the role reads (revision 2.4
		// candidate 24); a role that cannot read sends none.
		if (method == http.MethodPatch || method == http.MethodPut) && versionedPath.MatchString(path) {
			if v := c.version(path); v != "" {
				req.Header.Set("If-Match", v)
			}
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return resp.StatusCode, b
}

// caller returns who sends op for role: a session that ends or changes
// runs on its own login.
func (f *rightsFixture) caller(op matrixOp, role string) string {
	f.t.Helper()
	if !op.last || role == rX {
		return role
	}
	pw := matrixPassword
	if role == rP {
		pw = f.pwchangeTemp
	}
	f.c["fresh"] = login(f.t, f.base, role, pw)
	return "fresh"
}

// as returns the client of a role; it fails when the call fails.
func (f *rightsFixture) as(role string) *client { return f.c[role] }

func (f *rightsFixture) invite(by, role, name string) string {
	f.t.Helper()
	var inv struct{ Token string }
	f.as(by).must(http.MethodPost, "/invites", map[string]string{"role": role}, &inv, http.StatusCreated)
	var u struct{ ID string }
	anon := &client{t: f.t, base: f.base}
	anon.must(http.MethodPost, "/invites/redeem", map[string]string{"token": inv.Token, "pseudonym": name, "password": matrixPassword}, &u, http.StatusCreated)
	return u.ID
}

func (f *rightsFixture) freshServer() string {
	f.t.Helper()
	var s struct{ ID string }
	f.as(rA).must(http.MethodPost, "/servers", map[string]any{
		"name": f.unique("srv"), "profile_id": f.profile(), "host": "127.0.0.1", "port": 1, "password": "x",
	}, &s, http.StatusCreated)
	return s.ID
}

// template is a command template of the server for the matrix, made
// once; freshTemplate makes a new one.
func (f *rightsFixture) template() string {
	if f.tmpl == "" {
		f.tmpl = f.freshTemplate()
	}
	return f.tmpl
}

func (f *rightsFixture) freshTemplate() string {
	f.t.Helper()
	var t struct{ ID string }
	f.as(rA).must(http.MethodPost, "/servers/"+f.server+"/templates", map[string]string{
		"name": f.unique("tmpl"), "template": "say {text}",
	}, &t, http.StatusCreated)
	return t.ID
}

func (f *rightsFixture) profile() string {
	var p struct{ Items []struct{ ID, Slug string } }
	f.as(rA).must(http.MethodGet, "/profiles", nil, &p, http.StatusOK)
	for _, x := range p.Items {
		if x.Slug == "minecraft-java" {
			return x.ID
		}
	}
	f.t.Fatal("no minecraft profile")
	return ""
}

// agentRow puts an agent straight into the database (pairing has its own
// tests).
func (f *rightsFixture) agentRow(status string) string {
	f.t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(f.data, "rconductor.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	id := uuid.NewString()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES (?, 'host', x'00', ?, ?, ?)`,
		id, "fp-"+id, status, time.Now().UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func pngBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func newRightsFixture(t *testing.T) *rightsFixture {
	t.Helper()
	root := t.TempDir()
	cfg := testConfig(t, root)
	var out, console syncBuffer
	addr, stop := startPanel(t, cfg, &out, &console)
	t.Cleanup(func() { _ = stop() })
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	f := &rightsFixture{t: t, base: "http://" + addr, data: cfg.DataDir, c: map[string]*client{}, ids: map[string]string{}}
	if v, ok := agentAddrs.Load(cfg.SecretsDir); ok {
		f.agentAddr = v.(string)
	}

	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(console.String())
	admin := login(t, f.base, "admin", m[1])
	admin.must(http.MethodPost, "/me/password", map[string]string{"current_password": m[1], "new_password": matrixPassword}, nil, http.StatusNoContent)
	f.c[rA] = admin
	host, port, _ := net.SplitHostPort(game.Addr)
	p, _ := strconv.Atoi(port)
	var s struct{ ID string }
	admin.must(http.MethodPost, "/servers", map[string]any{"name": "main", "profile_id": f.profile(), "host": host, "port": p, "password": "rconpw"}, &s, http.StatusCreated)
	f.server = s.ID
	f.server2 = f.freshServer()
	var sg struct{ ID string }
	admin.must(http.MethodPost, "/server-groups", map[string]any{"name": "group-main", "server_ids": []string{f.server}}, &sg, http.StatusCreated)
	f.serverGroup = sg.ID
	admin.must(http.MethodPatch, "/settings", map[string]any{"supporters.enabled": true, "ratelimit.moderator.burst": 1000,
		"ratelimit.supporter.burst": 1000, "ratelimit.server_admin.burst": 1000, "ratelimit.rcon_admin.burst": 1000}, nil, http.StatusOK)

	for role, scope := range map[string]string{rSI: f.server, rSO: f.server2, rP: f.server} {
		f.ids[role] = f.invite(rA, "server_admin", role)
		admin.must(http.MethodPut, "/users/"+f.ids[role]+"/assignments", map[string]any{
			"admin_scopes": []map[string]string{{"server_id": scope}},
		}, nil, http.StatusOK)
		if role != rP {
			f.c[role] = login(t, f.base, role, matrixPassword)
		}
	}
	si := f.c[rSI]
	var list struct{ ID string }
	si.must(http.MethodPost, "/command-lists", map[string]any{"name": "mods", "entries": []map[string]any{
		{"path": "kick", "passable": false}, {"path": "say", "passable": true}, {"path": "time set", "passable": true},
	}}, &list, http.StatusCreated)
	f.list = list.ID
	var g struct{ ID string }
	si.must(http.MethodPost, "/user-groups", map[string]any{"name": "mods", "kind": "moderator", "command_list_id": f.list}, &g, http.StatusCreated)
	f.group = g.ID
	for _, role := range []string{rMR, rMN} {
		f.ids[role] = f.invite(rSI, "moderator", role)
		si.must(http.MethodPut, "/users/"+f.ids[role]+"/assignments", map[string]any{
			"groups": []string{f.group}, "grants": []map[string]string{{"group_id": f.group, "server_id": f.server}},
		}, nil, http.StatusOK)
		f.c[role] = login(t, f.base, role, matrixPassword)
	}
	si.must(http.MethodPatch, "/users/"+f.ids[rMR], map[string]bool{"supporters_allowed": true}, nil, http.StatusOK)
	// A changed release ends the moderator's sessions (D-044).
	f.c[rMR] = login(t, f.base, rMR, matrixPassword)
	f.ids[rSU] = f.invite(rMR, "supporter", rSU)
	f.as(rMR).must(http.MethodPut, "/users/"+f.ids[rSU]+"/assignments", map[string]any{
		"supporter_grants": []map[string]string{{"moderator_id": f.ids[rMR], "server_id": f.server}},
	}, nil, http.StatusOK)
	f.c[rSU] = login(t, f.base, rSU, matrixPassword)
	if status, _ := f.raw(rSU, http.MethodPut, "/me/avatar", pngBytes()); status >= 300 {
		t.Fatalf("avatar of sup: %d", status)
	}

	var reset struct{ Password string }
	admin.must(http.MethodPost, "/users/"+f.ids[rP]+"/password-reset", nil, &reset, http.StatusOK)
	f.pwchangeTemp = reset.Password
	f.c[rP] = login(t, f.base, rP, reset.Password)
	f.c[rX] = &client{t: t, base: f.base}
	return f
}

// outcome classes of the matrix.
//
//	O  allowed: 2xx
//	U  401 unauthenticated
//	P  403 password_change_required (AUTH-010)
//	F  403 forbidden or another refusal of the role
//	N  404, outside the scope (Design 3.1)
func classify(status int, reason string) byte {
	switch {
	case status >= 200 && status < 300:
		return 'O'
	case status == http.StatusUnauthorized:
		return 'U'
	case status == http.StatusForbidden && reason == "password_change_required":
		return 'P'
	case status == http.StatusForbidden:
		return 'F'
	case status == http.StatusNotFound:
		return 'N'
	case status == http.StatusServiceUnavailable && reason == "agent_unavailable":
		// Rights passed; the server has no connected agent.
		return 'A'
	case status == http.StatusUnprocessableEntity && reason == "invalid":
		// Rights passed; the input or the setup is not complete.
		return 'I'
	case status == http.StatusConflict && reason == "conflict":
		// Rights passed; the state does not allow it (AUTH-008).
		return 'C'
	}
	return '?'
}

// request is what one role sends for an operation.
type request struct {
	path string
	body any
}

// matrixOp is one row: the operation, the expected outcome per role in
// the order of matrixRoles, and how a role's request looks. Operations
// that change or remove something get a fresh target per role.
type matrixOp struct {
	method, path string
	expect       string
	req          func(f *rightsFixture, role string) request
	// last runs after all others (it ends sessions).
	last bool
}

func at(path string) func(*rightsFixture, string) request {
	return func(*rightsFixture, string) request { return request{path: path} }
}

func matrix() []matrixOp {
	userPath := func(suffix string) func(*rightsFixture, string) request {
		return func(f *rightsFixture, _ string) request { return request{path: "/users/" + f.ids[rSU] + suffix} }
	}
	// freshSupporter is a supporter the role itself manages, or sup for
	// roles that manage nobody.
	freshSupporter := func(f *rightsFixture, role string) string {
		switch role {
		case rA, rSI, rMR:
			id := f.invite(role, "supporter", f.unique("s"))
			if role != rMR {
				return id
			}
			f.as(rMR).must(http.MethodPut, "/users/"+id+"/assignments", map[string]any{
				"supporter_grants": []map[string]string{{"moderator_id": f.ids[rMR], "server_id": f.server}},
			}, nil, http.StatusOK)
			return id
		}
		return f.ids[rSU]
	}
	serverPath := func(suffix string) func(*rightsFixture, string) request {
		return func(f *rightsFixture, _ string) request { return request{path: "/servers/" + f.server + suffix} }
	}
	return []matrixOp{
		// last: logging in as sa_in with sa_in's own cookie sent along ends
		// that session (AUTH-006, D-052).
		{"POST", "/auth/login", "OOOOOOOP", func(f *rightsFixture, _ string) request {
			return request{path: "/auth/login", body: map[string]string{"pseudonym": rSI, "password": matrixPassword}}
		}, true},
		{"GET", "/auth/session", "OOOOOOUO", at("/auth/session"), false},
		// Second factor (AUTH-008): the second step without a waiting
		// login and a made-up link are refused for everyone; the recovery
		// request answers alike.
		{"POST", "/auth/login/totp", "UUUUUUUP", func(*rightsFixture, string) request {
			return request{path: "/auth/login/totp", body: map[string]string{"code": "000000"}}
		}, false},
		{"POST", "/auth/recovery", "OOOOOOOP", func(*rightsFixture, string) request {
			return request{path: "/auth/recovery", body: map[string]string{"pseudonym": "nobody"}}
		}, false},
		{"POST", "/auth/recovery/redeem", "UUUUUUUP", func(*rightsFixture, string) request {
			return request{path: "/auth/recovery/redeem", body: map[string]string{"token": "made-up", "password": matrixPassword}}
		}, false},
		{"POST", "/auth/logout", "OOOOOOUO", at("/auth/logout"), true},
		{"GET", "/profiles", "OOOOOOUP", at("/profiles"), false},

		{"GET", "/servers", "OOOOOOUP", at("/servers"), false},
		{"POST", "/servers", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers", body: map[string]any{"name": f.unique("new"), "profile_id": f.profile(), "host": "127.0.0.1", "port": 2, "password": "x"}}
		}, false},
		{"POST", "/servers/test-connection", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/test-connection", body: map[string]any{"profile_id": f.profile(), "host": "127.0.0.1", "port": 1, "password": "x"}}
		}, false},
		{"GET", "/servers/{id}", "OONOOOUP", serverPath(""), false},
		{"PATCH", "/servers/{id}", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/" + f.server, body: map[string]any{"name": "main"}}
		}, false},
		{"DELETE", "/servers/{id}", "OFFFFFUP", func(f *rightsFixture, role string) request {
			if role == rA {
				return request{path: "/servers/" + f.freshServer()}
			}
			return request{path: "/servers/" + f.server}
		}, false},
		{"GET", "/servers/{id}/commands", "OONOOOUP", serverPath("/commands"), false},
		{"POST", "/servers/{id}/commands", "OONOOOUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/" + f.server + "/commands", body: map[string]string{"command": "say hi"}}
		}, false},
		// One command to several servers (CMD-010): admins only; a server
		// outside the scope is a result not_found, not a refusal.
		{"POST", "/commands/multi", "OOOFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/commands/multi", body: map[string]any{"server_ids": []string{f.server}, "command": "say hi"}}
		}, false},
		// Command templates (CMD-008): everyone in scope reads, admins
		// change.
		{"GET", "/servers/{id}/templates", "OONOOOUP", serverPath("/templates"), false},
		{"POST", "/servers/{id}/templates", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/" + f.server + "/templates", body: map[string]string{"name": f.unique("t"), "template": "say {text}"}}
		}, false},
		{"GET", "/servers/{id}/templates/{template_id}", "OONOOOUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/" + f.server + "/templates/" + f.template()}
		}, false},
		{"PATCH", "/servers/{id}/templates/{template_id}", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/" + f.server + "/templates/" + f.template(), body: map[string]string{"template": "say {text}"}}
		}, false},
		{"DELETE", "/servers/{id}/templates/{template_id}", "OONFFFUP", func(f *rightsFixture, role string) request {
			if role == rA || role == rSI {
				return request{path: "/servers/" + f.server + "/templates/" + f.freshTemplate()}
			}
			return request{path: "/servers/" + f.server + "/templates/" + f.template()}
		}, false},
		{"GET", "/servers/{id}/players", "OONOOOUP", serverPath("/players"), false},
		{"GET", "/servers/{id}/history", "OONOOOUP", serverPath("/history"), false},
		{"POST", "/servers/{id}/actions", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/servers/" + f.server + "/actions", body: map[string]string{"action": "restart"}}
		}, false},
		// The server has no agent: admins pass the rights and get 503
		// agent_unavailable (revision 2.4 candidate 20).
		{"GET", "/servers/{id}/logs", "AANFFFUP", serverPath("/logs"), false},

		{"GET", "/server-groups", "OOOFFFUP", at("/server-groups"), false},
		{"POST", "/server-groups", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/server-groups", body: map[string]any{"name": f.unique("sg")}}
		}, false},
		{"GET", "/server-groups/{id}", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/server-groups/" + f.serverGroup}
		}, false},
		{"PATCH", "/server-groups/{id}", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/server-groups/" + f.serverGroup, body: map[string]any{"name": "group-main"}}
		}, false},
		{"DELETE", "/server-groups/{id}", "OFFFFFUP", func(f *rightsFixture, role string) request {
			if role == rA {
				var g struct{ ID string }
				f.as(rA).must(http.MethodPost, "/server-groups", map[string]any{"name": f.unique("sg")}, &g, http.StatusCreated)
				return request{path: "/server-groups/" + g.ID}
			}
			return request{path: "/server-groups/" + f.serverGroup}
		}, false},

		{"GET", "/agents", "OFFFFFUP", at("/agents"), false},
		{"POST", "/agents/tokens", "OFFFFFUP", at("/agents/tokens"), false},
		{"GET", "/agents/{id}", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/agents/" + f.agentRow("active")}
		}, false},
		{"DELETE", "/agents/{id}", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/agents/" + f.agentRow("active")}
		}, false},
		{"GET", "/agents/{id}/containers", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/agents/" + f.agentRow("active") + "/containers"}
		}, false},
		{"POST", "/agents/{id}/approve", "OFFFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/agents/" + f.agentRow("pending") + "/approve"}
		}, false},

		{"GET", "/users", "OOOOFFUP", at("/users"), false},
		{"GET", "/users/{id}", "OONOFFUP", userPath(""), false},
		{"PATCH", "/users/{id}", "OONOFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/users/" + f.ids[rSU], body: map[string]any{"disabled": false}}
		}, false},
		{"DELETE", "/users/{id}", "OONOFFUP", func(f *rightsFixture, role string) request {
			return request{path: "/users/" + freshSupporter(f, role)}
		}, false},
		{"GET", "/users/{id}/assignments", "OONOFFUP", userPath("/assignments"), false},
		{"PUT", "/users/{id}/assignments", "OONOFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/users/" + f.ids[rSU] + "/assignments", body: map[string]any{}}
		}, false},
		{"POST", "/users/{id}/password-reset", "OONOFFUP", func(f *rightsFixture, role string) request {
			return request{path: "/users/" + freshSupporter(f, role) + "/password-reset"}
		}, false},
		{"DELETE", "/users/{id}/second-factor", "OONOFFUP", func(f *rightsFixture, role string) request {
			return request{path: "/users/" + freshSupporter(f, role) + "/second-factor"}
		}, false},
		{"GET", "/users/{id}/avatar", "OONONOUP", userPath("/avatar"), false},
		// A fresh supporter, so that the matrix keeps its own sessions.
		{"DELETE", "/users/{id}/sessions", "OONOFFUP", func(f *rightsFixture, role string) request {
			return request{path: "/users/" + freshSupporter(f, role) + "/sessions"}
		}, false},

		{"GET", "/user-groups", "OOOFFFUP", at("/user-groups"), false},
		{"POST", "/user-groups", "OOOFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/user-groups", body: map[string]any{"name": f.unique("ug"), "kind": "moderator"}}
		}, false},
		{"GET", "/user-groups/{id}", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/user-groups/" + f.group}
		}, false},
		{"PATCH", "/user-groups/{id}", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/user-groups/" + f.group, body: map[string]any{"name": "mods"}}
		}, false},
		{"DELETE", "/user-groups/{id}", "OONFFFUP", func(f *rightsFixture, role string) request {
			if role == rA || role == rSI {
				var g struct{ ID string }
				f.as(role).must(http.MethodPost, "/user-groups", map[string]any{"name": f.unique("ug"), "kind": "moderator"}, &g, http.StatusCreated)
				return request{path: "/user-groups/" + g.ID}
			}
			return request{path: "/user-groups/" + f.group}
		}, false},

		{"GET", "/command-lists", "OOOFFFUP", at("/command-lists"), false},
		{"POST", "/command-lists", "OOOFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/command-lists", body: map[string]any{"name": f.unique("cl")}}
		}, false},
		{"GET", "/command-lists/{id}", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/command-lists/" + f.list}
		}, false},
		{"PATCH", "/command-lists/{id}", "OONFFFUP", func(f *rightsFixture, _ string) request {
			return request{path: "/command-lists/" + f.list, body: map[string]any{"name": "mods"}}
		}, false},
		{"DELETE", "/command-lists/{id}", "OONFFFUP", func(f *rightsFixture, role string) request {
			if role == rA || role == rSI {
				var l struct{ ID string }
				f.as(role).must(http.MethodPost, "/command-lists", map[string]any{"name": f.unique("cl")}, &l, http.StatusCreated)
				return request{path: "/command-lists/" + l.ID}
			}
			return request{path: "/command-lists/" + f.list}
		}, false},

		{"GET", "/invites", "OOOOFFUP", at("/invites"), false},
		{"POST", "/invites", "OOOOFFUP", func(*rightsFixture, string) request {
			return request{path: "/invites", body: map[string]string{"role": "supporter"}}
		}, false},
		{"DELETE", "/invites/{id}", "OOOOFFUP", func(f *rightsFixture, role string) request {
			by := role
			if !slices.Contains([]string{rA, rSI, rSO, rMR}, role) {
				by = rA
			}
			var inv struct{ Invite struct{ ID string } }
			f.as(by).must(http.MethodPost, "/invites", map[string]string{"role": "supporter"}, &inv, http.StatusCreated)
			return request{path: "/invites/" + inv.Invite.ID}
		}, false},
		{"POST", "/invites/redeem", "OOOOOOOP", func(f *rightsFixture, _ string) request {
			var inv struct{ Token string }
			f.as(rA).must(http.MethodPost, "/invites", map[string]string{"role": "supporter"}, &inv, http.StatusCreated)
			return request{path: "/invites/redeem", body: map[string]string{"token": inv.Token, "pseudonym": f.unique("r"), "password": matrixPassword}}
		}, false},

		{"GET", "/me", "OOOOOOUP", at("/me"), false},
		// The own second factor: before any setup confirm and switching off
		// find nothing (409), then the setup with the password works.
		{"GET", "/me/second-factor", "OOOOOOUP", at("/me/second-factor"), false},
		{"POST", "/me/second-factor/confirm", "CCCCCCUP", func(*rightsFixture, string) request {
			return request{path: "/me/second-factor/confirm", body: map[string]string{"code": "000000"}}
		}, false},
		{"POST", "/me/second-factor/disable", "CCCCCCUP", func(*rightsFixture, string) request {
			return request{path: "/me/second-factor/disable", body: map[string]string{"password": matrixPassword}}
		}, false},
		{"POST", "/me/second-factor/setup", "OOOOOOUP", func(*rightsFixture, string) request {
			return request{path: "/me/second-factor/setup", body: map[string]string{"password": matrixPassword}}
		}, false},
		{"PUT", "/me/email", "OOOOOOUP", func(*rightsFixture, string) request {
			return request{path: "/me/email", body: map[string]string{"email": "", "password": matrixPassword}}
		}, false},
		{"PATCH", "/me", "OOOOOOUP", func(*rightsFixture, string) request {
			return request{path: "/me", body: map[string]any{"real_name": "Real Name"}}
		}, false},
		{"GET", "/me/favorites", "OOOOOOUP", at("/me/favorites"), false},
		{"PUT", "/me/favorites", "OOOOOOUP", func(*rightsFixture, string) request {
			return request{path: "/me/favorites", body: map[string]any{"server_ids": []string{}}}
		}, false},
		{"PUT", "/me/avatar", "OOOOOOUP", func(*rightsFixture, string) request {
			return request{path: "/me/avatar", body: pngBytes()}
		}, false},
		{"DELETE", "/me/avatar", "OOOOOOUP", at("/me/avatar"), false},
		{"POST", "/me/password", "OOOOOOUO", func(f *rightsFixture, role string) request {
			current := matrixPassword
			if role == rP {
				current = f.pwchangeTemp
			}
			return request{path: "/me/password", body: map[string]string{"current_password": current, "new_password": matrixPassword + " new"}}
		}, true},

		{"GET", "/audit", "OOOFFFUP", at("/audit"), false},
		{"GET", "/settings", "OFFFFFUP", at("/settings"), false},
		{"PATCH", "/settings", "OFFFFFUP", func(*rightsFixture, string) request {
			return request{path: "/settings", body: map[string]any{"ui.shortcuts_enabled": false}}
		}, false},
		// SMTP (NFR-017): RCON admin only; the test mail needs a set-up
		// server, which the fixture has not, so the RCON admin gets 422.
		{"GET", "/settings/smtp", "OFFFFFUP", at("/settings/smtp"), false},
		{"PUT", "/settings/smtp", "OFFFFFUP", func(*rightsFixture, string) request {
			return request{path: "/settings/smtp", body: map[string]any{"port": 587}}
		}, false},
		{"POST", "/settings/smtp/test", "IFFFFFUP", func(*rightsFixture, string) request {
			return request{path: "/settings/smtp/test", body: map[string]any{"to": "someone@example.org"}}
		}, false},
		// /ws is checked with a real upgrade below; the row keeps the
		// matrix complete.
		{"GET", "/ws", "OOOOOOUP", nil, false},
	}
}

// openAPIOperations lists "METHOD path" of every operation in the API.
func openAPIOperations(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	for path, ops := range doc.Paths {
		for method := range ops {
			if method == "parameters" {
				continue
			}
			out = append(out, strings.ToUpper(method)+" "+path)
		}
	}
	slices.Sort(out)
	return out
}

// TestRightsMatrix checks every operation of api/openapi.yaml for every
// role against the expectation of Design 2.2, 3.1 and 3.6 (and the
// revision 2.4 candidates for the newer endpoints). An operation without a
// row fails the test.
func TestRightsMatrix(t *testing.T) {
	ops := matrix()
	rows := map[string]bool{}
	for _, op := range ops {
		if len(op.expect) != len(matrixRoles) {
			t.Fatalf("%s %s: expectation %q has not %d roles", op.method, op.path, op.expect, len(matrixRoles))
		}
		rows[op.method+" "+op.path] = true
	}
	for _, op := range openAPIOperations(t) {
		if !rows[op] {
			t.Errorf("operation %s has no row in the rights matrix", op)
		}
	}
	for op := range rows {
		if !slices.Contains(openAPIOperations(t), op) {
			t.Errorf("matrix row %s is not in the API", op)
		}
	}
	if t.Failed() {
		return
	}

	f := newRightsFixture(t)
	ordered := slices.Clone(ops)
	slices.SortStableFunc(ordered, func(a, b matrixOp) int {
		switch {
		case a.last == b.last:
			return 0
		case a.last:
			return 1
		}
		return -1
	})
	for _, op := range ordered {
		if op.req == nil {
			continue
		}
		for i, role := range matrixRoles {
			r := op.req(f, role)
			status, reason := f.raw(f.caller(op, role), op.method, r.path, r.body)
			if got, want := classify(status, reason), op.expect[i]; got != want {
				t.Errorf("%s %s as %s: %d %s (%c), want %c", op.method, op.path, role, status, reason, got, want)
			}
		}
	}
}

// TestGameVersionAndVolumesByRole extends the matrix by the agent data of
// 1.1: the game version is a server datum for every role that sees the
// server (AGT-006, UI-006), the volumes with their sizes only for RCON
// admin and the server admin in scope (AGT-007, UI-006); outside the scope
// the server does not exist.
func TestGameVersionAndVolumesByRole(t *testing.T) {
	f := newRightsFixture(t)
	pairAgent(t, f.as(rA), f.agentAddr, f.server, &hostBackend{caps: []string{agentproto.CapVersionDetect, agentproto.CapVolumesSize}})
	type server struct {
		Runtime struct {
			GameVersion string `json:"game_version"`
			Container   *struct {
				Volumes []struct {
					Name      string `json:"name"`
					SizeBytes *int64 `json:"size_bytes"`
				} `json:"volumes"`
			} `json:"container"`
		}
	}
	// The panel asks the agent in the background.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var s server
		f.as(rA).must(http.MethodGet, "/servers/"+f.server, nil, &s, http.StatusOK)
		c := s.Runtime.Container
		if s.Runtime.GameVersion != "" && c != nil && len(c.Volumes) == 1 && c.Volumes[0].SizeBytes != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no version or size: %+v", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, role := range []string{rA, rSI, rSO, rMR, rMN, rSU} {
		var s server
		status := f.as(role).call(http.MethodGet, "/servers/"+f.server, nil, &s)
		switch role {
		case rSO:
			if status != http.StatusNotFound {
				t.Errorf("%s: %d, want 404", role, status)
			}
		case rA, rSI:
			c := s.Runtime.Container
			if status != http.StatusOK || s.Runtime.GameVersion != "1.21.4" || c == nil || len(c.Volumes) != 1 ||
				c.Volumes[0].Name != "mc-data" || *c.Volumes[0].SizeBytes != 1<<30 {
				t.Errorf("%s: %d %+v", role, status, s.Runtime)
			}
		default:
			if status != http.StatusOK || s.Runtime.GameVersion != "1.21.4" || s.Runtime.Container != nil {
				t.Errorf("%s: %d %+v, want the version and no container part", role, status, s.Runtime)
			}
		}
	}
}

// TestPortsAndTopologyByRole extends the matrix by UI-005 and UI-010: the
// RCON admin sees game ports, the RCON port and every published port, the
// server admin in scope only the game ports, moderator and supporter none;
// for the topology the server admin gets the agent's state but never its
// name (F10), the RCON admin both.
func TestPortsAndTopologyByRole(t *testing.T) {
	f := newRightsFixture(t)
	pairAgent(t, f.as(rA), f.agentAddr, f.server, &hostBackend{ports: []agentproto.Port{
		{Private: 25565, Public: 30000, Proto: "tcp"}, {Private: 8123, Public: 8123, Proto: "tcp"},
	}})
	kinds := func(role string) (int, []string) {
		var s struct {
			Runtime struct {
				Container *struct {
					Ports []struct{ Kind string }
				}
			}
		}
		status := f.as(role).call(http.MethodGet, "/servers/"+f.server, nil, &s)
		if s.Runtime.Container == nil {
			return status, nil
		}
		var out []string
		for _, p := range s.Runtime.Container.Ports {
			out = append(out, p.Kind)
		}
		return status, out
	}
	for role, want := range map[string][]string{
		rA:  {"game", "rcon", "other"},
		rSI: {"game"},
		rMR: nil, rMN: nil, rSU: nil,
	} {
		if status, got := kinds(role); status != http.StatusOK || !slices.Equal(got, want) {
			t.Errorf("%s: %d ports %v, want %v", role, status, got, want)
		}
	}
	if status, _ := kinds(rSO); status != http.StatusNotFound {
		t.Errorf("%s: %d, want 404", rSO, status)
	}

	// Topology: the server admin reads no agent, so no name reaches him.
	if status, _ := f.raw(rSI, http.MethodGet, "/agents", nil); status != http.StatusForbidden {
		t.Errorf("server admin reads the agents: %d", status)
	}
	for _, path := range []string{"/servers", "/servers/" + f.server} {
		status, b := f.rawBody(rSI, http.MethodGet, path, nil)
		body := string(b)
		if status != http.StatusOK || strings.Contains(body, "host-1") || !strings.Contains(body, `"agent_connected":true`) {
			t.Errorf("server admin %s: %d %s", path, status, body)
		}
	}
	if status, b := f.rawBody(rA, http.MethodGet, "/agents", nil); status != http.StatusOK || !strings.Contains(string(b), "host-1") {
		t.Errorf("RCON admin agents: %d %s", status, b)
	}
}

// TestCapabilitiesByRole extends the matrix by the field capabilities of
// the runtime data (AGT-013): RCON admin and the server admin in scope see
// it with the container part, moderator and supporter get neither
// (AGT-008, UI-006), outside the scope the server does not exist; the
// same in the server list.
func TestCapabilitiesByRole(t *testing.T) {
	f := newRightsFixture(t)
	pairAgent(t, f.as(rA), f.agentAddr, f.server, &hostBackend{caps: []string{agentproto.CapLogs}})
	type runtime struct {
		Container *struct {
			Capabilities *[]string `json:"capabilities"`
		} `json:"container"`
	}
	for _, role := range []string{rA, rSI, rSO, rMR, rMN, rSU} {
		var one struct{ Runtime runtime }
		status := f.as(role).call(http.MethodGet, "/servers/"+f.server, nil, &one)
		var list struct {
			Items []struct {
				ID      string
				Runtime runtime
			}
		}
		f.as(role).must(http.MethodGet, "/servers", nil, &list, http.StatusOK)
		var inList *runtime
		for i := range list.Items {
			if list.Items[i].ID == f.server {
				inList = &list.Items[i].Runtime
			}
		}
		switch role {
		case rA, rSI:
			for where, rt := range map[string]*runtime{"server": &one.Runtime, "list": inList} {
				if status != http.StatusOK || rt == nil || rt.Container == nil || rt.Container.Capabilities == nil ||
					!slices.Equal(*rt.Container.Capabilities, []string{agentproto.CapLogs}) {
					t.Errorf("%s %s: status %d, runtime %+v, want capabilities [logs]", role, where, status, rt)
				}
			}
		case rSO:
			if status != http.StatusNotFound || inList != nil {
				t.Errorf("%s: status %d, in list %v, want 404 and not listed", role, status, inList != nil)
			}
		default:
			if status != http.StatusOK || one.Runtime.Container != nil || inList == nil || inList.Container != nil {
				t.Errorf("%s: status %d, container part %+v, want none", role, status, one.Runtime.Container)
			}
		}
	}
}

// TestForbiddenHidesExistence checks the order of the rights check
// (revision 2.4 candidate 14): a role that may not use a kind of resource
// gets 403 before any lookup of the object, so the answer for an existing
// and a missing ID is the same in status and body.
func TestForbiddenHidesExistence(t *testing.T) {
	f := newRightsFixture(t)
	param := regexp.MustCompile(`^\{[^}]+\}$`)
	checked := 0
	for _, op := range matrix() {
		if op.req == nil || !strings.Contains(op.path, "{") {
			continue
		}
		for i, role := range matrixRoles {
			if op.expect[i] != 'F' {
				continue
			}
			r := op.req(f, role)
			// The same path with every ID swapped for one that does not exist.
			tmpl, segs := strings.Split(op.path, "/"), strings.Split(r.path, "/")
			if len(tmpl) != len(segs) {
				t.Fatalf("%s: path %s does not match the template", op.path, r.path)
			}
			missing := slices.Clone(segs)
			for j, seg := range tmpl {
				if param.MatchString(seg) {
					missing[j] = uuid.NewString()
				}
			}
			var bodies [2]string
			var statuses [2]int
			for k, path := range []string{r.path, strings.Join(missing, "/")} {
				status, b := f.rawBody(f.caller(op, role), op.method, path, r.body)
				statuses[k], bodies[k] = status, withoutRequestID(t, b)
			}
			if statuses[0] != http.StatusForbidden || statuses != [2]int{statuses[0], statuses[0]} || bodies[0] != bodies[1] {
				t.Errorf("%s %s as %s: existing %d %s, missing %d %s",
					op.method, op.path, role, statuses[0], bodies[0], statuses[1], bodies[1])
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no forbidden operation with an ID checked")
	}
}

// withoutRequestID returns a Problem Details body without its request ID,
// which differs on every response.
func withoutRequestID(t *testing.T, b []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	delete(m, "request_id")
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestWebSocketMatrix checks the upgrade and every topic for every role
// (Design 2.3): subscribed, or error with not_found, forbidden.
func TestWebSocketMatrix(t *testing.T) {
	f := newRightsFixture(t)
	topics := wsTopics(t)
	// expect per topic kind, roles in the order of matrixRoles (anon and
	// pwchange cannot open the socket).
	want := map[string]string{
		"servers.overview": "SSSSSS",
		"status":           "SSNSSS",
		"console":          "SSNSSS",
		"metrics":          "SSNFFF",
		"logs":             "SSNFFF",
	}
	for _, topic := range topics {
		kind := topic
		if strings.HasPrefix(topic, "server.") {
			kind = topic[strings.LastIndex(topic, ".")+1:]
		}
		if _, ok := want[kind]; !ok {
			t.Errorf("topic %s has no row in the WebSocket matrix", topic)
		}
	}
	for i, role := range matrixRoles {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		h := http.Header{"Origin": {f.base}}
		if c := f.c[role]; c != nil && c.cookie != nil {
			h.Set("Cookie", c.cookie.String())
		}
		conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.base, "http")+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: h})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		switch role {
		case rX, rP:
			wantStatus := map[string]int{rX: http.StatusUnauthorized, rP: http.StatusForbidden}[role]
			if err == nil || resp == nil || resp.StatusCode != wantStatus {
				t.Errorf("ws as %s: %v %v, want %d", role, resp, err, wantStatus)
			}
			cancel()
			continue
		}
		if err != nil {
			t.Fatalf("ws as %s: %v", role, err)
		}
		for _, topic := range topics {
			kind := topic
			if strings.HasPrefix(topic, "server.") {
				kind = topic[strings.LastIndex(topic, ".")+1:]
			}
			path := strings.ReplaceAll(topic, "{id}", f.server)
			b, _ := json.Marshal(map[string]string{"type": "subscribe", "topic": path})
			if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
				t.Fatal(err)
			}
			var m struct{ Type, Topic, Reason string }
			for {
				if err := readJSON(conn, &m); err != nil {
					t.Fatalf("ws %s as %s: %v", path, role, err)
				}
				if m.Topic == path && (m.Type == "subscribed" || m.Type == "error") {
					break
				}
			}
			got := map[string]byte{"subscribed": 'S'}[m.Type]
			if m.Type == "error" {
				got = map[string]byte{"not_found": 'N', "forbidden": 'F'}[m.Reason]
			}
			if exp := want[kind][i]; got != exp {
				t.Errorf("ws %s as %s: %s %s, want %c", path, role, m.Type, m.Reason, exp)
			}
		}
		_ = conn.CloseNow()
		cancel()
	}
}

// wsTopics reads the topics from the description of /ws in the API.
func wsTopics(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("  /ws:"))
	desc := string(b[i : i+3000])
	seen := map[string]bool{}
	var out []string
	for _, m := range regexp.MustCompile(`servers\.overview|server\.\{id\}\.[a-z]+`).FindAllString(desc, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) < 5 {
		t.Fatalf("topics from the API: %v", out)
	}
	return out
}

// TestCatalogParams covers revision 2.4 candidate 19 over the API: for
// moderator and supporter a command with catalog entry must fit its
// parameters (422 invalid with the parameter in the detail, audited as
// cmd.denied); admins keep free input.
func TestCatalogParams(t *testing.T) {
	f := newRightsFixture(t)
	path := "/servers/" + f.server + "/commands"
	cases := []struct {
		role, command string
		status        int
		detail        string
	}{
		{rA, "time set dusk", http.StatusOK, ""},
		{rSI, "time set dusk", http.StatusOK, ""},
		{rMR, "time set dusk", http.StatusUnprocessableEntity, "parameter time"},
		{rSU, "time set dusk", http.StatusUnprocessableEntity, "parameter time"},
		{rMR, "time set day", http.StatusOK, ""},
		{rMR, "kick", http.StatusUnprocessableEntity, "parameter player: missing"},
		{rMR, "kick steve spamming in chat", http.StatusOK, ""},
		{rMR, "kick @a", http.StatusUnprocessableEntity, "parameter player: not a player name"},
		{rMR, "kick @e[type=player]", http.StatusUnprocessableEntity, "parameter player: not a player name"},
		{rMR, `kick "x"`, http.StatusUnprocessableEntity, "parameter player: not a player name"},
		{rMR, "time set day extra", http.StatusUnprocessableEntity, "unexpected argument"},
		{rA, "time set day extra", http.StatusOK, ""},
	}
	for _, c := range cases {
		status, body := f.rawBody(c.role, http.MethodPost, path, map[string]string{"command": c.command})
		var p struct{ Reason, Detail string }
		_ = json.Unmarshal(body, &p)
		if status != c.status || (c.detail != "" && (p.Reason != "invalid" || !strings.Contains(p.Detail, c.detail))) {
			t.Errorf("%s %q = %d %s, want %d %q", c.role, c.command, status, body, c.status, c.detail)
		}
	}
	var audit struct {
		Items []struct {
			Action, Result, Reason string
			ActorRole              string `json:"actor_role"`
		}
	}
	f.as(rA).must(http.MethodGet, "/audit?kind=command", nil, &audit, http.StatusOK)
	denied := 0
	for _, e := range audit.Items {
		if e.Action == "cmd.denied" && e.Reason == "invalid" {
			denied++
		}
	}
	if denied != 7 {
		t.Errorf("cmd.denied with invalid = %d, want 7: %+v", denied, audit.Items)
	}
}

// guarded sends a write with If-Match as role and returns status, reason
// and the response headers.
func (f *rightsFixture) guarded(c *client, method, path, ifMatch string, body any) (int, string, http.Header) {
	f.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(context.Background(), method, f.base+"/api/v1"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", f.base)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var p struct{ Reason, Detail string }
	_ = json.NewDecoder(resp.Body).Decode(&p)
	if p.Detail != "" && resp.StatusCode != http.StatusConflict {
		f.t.Logf("%s %s: %s", method, path, p.Detail)
	}
	return resp.StatusCode, p.Reason, resp.Header
}

// TestOptimisticLocking covers revision 2.4 candidate 24 with two
// sessions of the RCON admin: a write naming a stale version is refused
// with 409 conflict and writes nothing; the current version passes; a
// write without If-Match is refused with 428. For every object saved as
// a whole: user group, command list, server, server group, settings.
func TestOptimisticLocking(t *testing.T) {
	f := newRightsFixture(t)
	one, two := f.as(rA), login(t, f.base, "admin", matrixPassword)
	type object struct {
		name    string
		path    string
		version func() string
		change  func(n int) any
		read    func() string
	}
	field := func(path, key string) func() string {
		return func() string {
			var m map[string]any
			one.must(http.MethodGet, path, nil, &m, http.StatusOK)
			s, _ := m[key].(string)
			return s
		}
	}
	settingsETag := func() string {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, f.base+"/api/v1/settings", nil)
		req.AddCookie(one.cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.Header.Get("ETag")
	}
	named := func(prefix string) func(n int) any {
		return func(n int) any { return map[string]any{"name": fmt.Sprintf("%s-%d", prefix, n)} }
	}
	objects := []object{
		{"user group", "/user-groups/" + f.group, field("/user-groups/"+f.group, "version"), named("lock-grp"), field("/user-groups/"+f.group, "name")},
		{"command list", "/command-lists/" + f.list, field("/command-lists/"+f.list, "version"), named("lock-lst"), field("/command-lists/"+f.list, "name")},
		{"server", "/servers/" + f.server, field("/servers/"+f.server, "version"), named("lock-srv"), field("/servers/"+f.server, "name")},
		{"server group", "/server-groups/" + f.serverGroup, field("/server-groups/"+f.serverGroup, "version"), named("lock-sg"), field("/server-groups/"+f.serverGroup, "name")},
		{"settings", "/settings", settingsETag, func(n int) any {
			return map[string]any{"invite.lifetime": fmt.Sprintf("%dh", 70+n)}
		}, func() string {
			var m map[string]any
			one.must(http.MethodGet, "/settings", nil, &m, http.StatusOK)
			s, _ := m["invite.lifetime"].(string)
			return s
		}},
	}
	for _, o := range objects {
		old := o.version()
		if old == "" {
			t.Fatalf("%s: no version", o.name)
		}
		// The second session changes the object with the current version.
		if st, reason, _ := f.guarded(two, http.MethodPatch, o.path, old, o.change(1)); st != http.StatusOK {
			t.Fatalf("%s: second session = %d %s", o.name, st, reason)
		}
		after := o.read()
		// The first still holds the old version: refused, nothing written.
		if st, reason, _ := f.guarded(one, http.MethodPatch, o.path, old, o.change(2)); st != http.StatusConflict || reason != "conflict" {
			t.Errorf("%s: stale write = %d %s, want 409 conflict", o.name, st, reason)
		}
		if o.read() != after {
			t.Errorf("%s: a stale write changed the object", o.name)
		}
		// With the version read again it passes; without If-Match it is
		// refused (428), and nothing is written.
		if st, reason, _ := f.guarded(one, http.MethodPatch, o.path, `"`+o.version()+`"`, o.change(3)); st != http.StatusOK {
			t.Errorf("%s: fresh write = %d %s", o.name, st, reason)
		}
		before := o.read()
		if st, reason, _ := f.guarded(one, http.MethodPatch, o.path, "", o.change(4)); st != http.StatusPreconditionRequired || reason != "precondition_required" {
			t.Errorf("%s: write without If-Match = %d %s, want 428 precondition_required", o.name, st, reason)
		}
		if o.read() != before {
			t.Errorf("%s: a write without If-Match changed the object", o.name)
		}
	}
	// The role comes before the version: a server admin may read the
	// server group but not change it, stale version, none or not; a
	// moderator gets 403 on a server without If-Match, not 428.
	for _, h := range []string{"stale", ""} {
		if st, reason, _ := f.guarded(f.as(rSI), http.MethodPatch, "/server-groups/"+f.serverGroup, h, map[string]any{"name": "x"}); st != http.StatusForbidden {
			t.Errorf("server admin with If-Match %q = %d %s, want 403", h, st, reason)
		}
	}
	if st, reason, _ := f.guarded(f.as(rMR), http.MethodPatch, "/servers/"+f.server, "", map[string]any{"name": "x"}); st != http.StatusForbidden {
		t.Errorf("moderator without If-Match = %d %s, want 403", st, reason)
	}
}

// TestIfMatchRequiredFromSpec reads the versioned writes from
// api/openapi.yaml: the operations with the parameter IfMatch, which carries
// x-rcd-required-by-server. Without If-Match each is refused with 428 for
// the RCON admin, while every other role with a session gets the answer of
// the rights matrix, never 428: role and scope come before the version
// (Design 2.1, 3.1, D-051). VersionedPath in web/src/api/client.ts must
// name the same paths, so the client cannot send one of them without
// If-Match (UI-007, UI-008, NFR-011).
func TestIfMatchRequiredFromSpec(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Parameters map[string]map[string]any `yaml:"parameters"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	param := doc.Components.Parameters["IfMatch"]
	if param["x-rcd-required-by-server"] != true {
		t.Fatalf("parameter IfMatch: x-rcd-required-by-server = %v, want true", param["x-rcd-required-by-server"])
	}
	// Required in the file would let the generated wrapper answer 400
	// before the rights check (D-051).
	if param["required"] != false {
		t.Fatalf("parameter IfMatch: required = %v, want false", param["required"])
	}
	const ref = "#/components/parameters/IfMatch"
	var versioned []string
	paths := map[string]bool{}
	for path, ops := range doc.Paths {
		for method, raw := range ops {
			op, _ := raw.(map[string]any)
			params, _ := op["parameters"].([]any)
			for _, p := range params {
				if m, _ := p.(map[string]any); m["$ref"] == ref {
					versioned = append(versioned, strings.ToUpper(method)+" "+path)
					paths[path] = true
				}
			}
		}
	}
	slices.Sort(versioned)
	if len(versioned) == 0 {
		t.Fatal("no operation uses the parameter IfMatch")
	}

	// The client lists the same paths.
	src, err := os.ReadFile("../../web/src/api/client.ts")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)export type VersionedPath =(.*?);`).FindSubmatch(src)
	if block == nil {
		t.Fatal("web/src/api/client.ts: type VersionedPath not found")
	}
	var client []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(block[1], -1) {
		client = append(client, string(m[1]))
	}
	slices.Sort(client)
	want := slices.Sorted(maps.Keys(paths))
	if !slices.Equal(client, want) {
		t.Errorf("VersionedPath in web/src/api/client.ts = %v, want the paths of api/openapi.yaml %v", client, want)
	}

	rows := map[string]matrixOp{}
	for _, op := range matrix() {
		rows[op.method+" "+op.path] = op
	}
	f := newRightsFixture(t)
	for _, name := range versioned {
		row, ok := rows[name]
		if !ok || row.req == nil {
			t.Errorf("%s: no request in the rights matrix", name)
			continue
		}
		r := row.req(f, rA)
		// The test client names the version only for these paths.
		if !versionedPath.MatchString(r.path) {
			t.Errorf("%s: %s does not match versionedPath", name, r.path)
		}
		allowed := 0
		for i, role := range matrixRoles {
			c := f.c[role]
			if c == nil || c.cookie == nil {
				continue
			}
			r := row.req(f, role)
			st, reason, _ := f.guarded(c, row.method, r.path, "", r.body)
			switch want := row.expect[i]; {
			case want == 'O':
				// May write: refused only for the missing version.
				allowed++
				if st != http.StatusPreconditionRequired || reason != "precondition_required" {
					t.Errorf("%s as %s without If-Match = %d %s, want 428 precondition_required", name, role, st, reason)
				}
			case st == http.StatusPreconditionRequired || classify(st, reason) != want:
				// May not write: the rights answer comes first.
				t.Errorf("%s as %s without If-Match = %d %s, want %c of the rights matrix", name, role, st, reason, want)
			}
		}
		if allowed == 0 {
			t.Errorf("%s: no role may write it in the rights matrix", name)
		}
	}
}

// TestVersionIsAtomic: two writes with the same version at the same time,
// from two sessions; the database lets exactly one pass (revision 2.4
// candidate 24, NFR-011).
func TestVersionIsAtomic(t *testing.T) {
	f := newRightsFixture(t)
	sessions := []*client{f.as(rA), login(t, f.base, "admin", matrixPassword)}
	for round := range 10 {
		v := sessions[0].version("/command-lists/" + f.list)
		var wg sync.WaitGroup
		codes := make([]int, len(sessions))
		for i, c := range sessions {
			wg.Go(func() {
				codes[i], _, _ = f.guarded(c, http.MethodPatch, "/command-lists/"+f.list, v, map[string]any{"name": fmt.Sprintf("race-%d-%d", round, i)})
			})
		}
		wg.Wait()
		slices.Sort(codes)
		if !slices.Equal(codes, []int{http.StatusOK, http.StatusConflict}) {
			t.Fatalf("round %d: %v, want one 200 and one 409", round, codes)
		}
	}
}
