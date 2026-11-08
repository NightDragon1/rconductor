// SPDX-License-Identifier: AGPL-3.0-or-later

// Package httpapitest has a fake game server per http-api dialect for the
// common test suite (D-071). A new dialect adds its Fixture to Fixtures;
// the suite then runs for it as for every other.
package httpapitest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
)

// Fixture answers like the game server of one dialect.
type Fixture interface {
	// Handler accepts token only and answers a console command with run;
	// it reports players as the number online.
	Handler(token string, players int, run func(command string) string) http.Handler
}

// Fixtures holds one fake server per dialect, by dialect name.
var Fixtures = map[string]Fixture{
	"satisfactory": Satisfactory{},
}

// Server is a fake game server over TLS with its own certificate.
type Server struct {
	*httptest.Server
	// Fingerprint is the SHA-256 of the server's certificate.
	Fingerprint string
	// Authorized counts requests that carried an Authorization header.
	Authorized atomic.Int64
	// Requests counts all requests.
	Requests atomic.Int64
	// Redirect makes every answer a redirect to another host.
	Redirect atomic.Bool
}

// Start runs the fake server of a dialect.
func Start(f Fixture, token string, players int, run func(command string) string) *Server {
	s := &Server{}
	inner := f.Handler(token, players, run)
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Requests.Add(1)
		if r.Header.Get("Authorization") != "" {
			s.Authorized.Add(1)
		}
		if s.Redirect.Load() {
			http.Redirect(w, r, "https://elsewhere.invalid/api/v1", http.StatusFound)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	s.Fingerprint = httpapi.Fingerprint(s.Certificate().Raw)
	return s
}

// Addr is host:port of the server.
func (s *Server) Addr() string { return s.Listener.Addr().String() }

// Satisfactory is the fake Satisfactory dedicated server.
type Satisfactory struct{}

// Handler answers HealthCheck, QueryServerState and RunCommand on /api/v1.
func (Satisfactory) Handler(token string, players int, run func(string) string) http.Handler {
	write := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1" {
			write(w, http.StatusNotFound, map[string]string{"errorCode": "not_found"})
			return
		}
		var req struct {
			Function string `json:"function"`
			Data     struct {
				Command string `json:"command"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			write(w, http.StatusBadRequest, map[string]string{"errorCode": "invalid_request"})
			return
		}
		if req.Function == "HealthCheck" {
			write(w, http.StatusOK, map[string]any{"data": map[string]string{"health": "healthy", "serverCustomData": ""}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			write(w, http.StatusUnauthorized, map[string]string{"errorCode": "invalid_token", "errorMessage": "invalid token"})
			return
		}
		switch req.Function {
		case "QueryServerState":
			write(w, http.StatusOK, map[string]any{"data": map[string]any{"serverGameState": map[string]any{
				"activeSessionName": "test", "numConnectedPlayers": players, "playerLimit": 4,
			}}})
		case "RunCommand":
			write(w, http.StatusOK, map[string]any{"data": map[string]string{"commandResult": run(req.Data.Command)}})
		default:
			write(w, http.StatusBadRequest, map[string]string{"errorCode": "unknown_function"})
		}
	})
}
