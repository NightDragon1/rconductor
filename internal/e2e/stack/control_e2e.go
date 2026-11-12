// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// controlEnabled tells whether this build has the control listener.
const controlEnabled = true

// startControl listens on a free local port and serves control there; it
// returns the listener's URL and a function that stops it.
func startControl(ctx context.Context, panel, o, agentAddr, root string) (string, func(), error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: control(ctx, panel, o, agentAddr, root), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), func() { _ = srv.Close() }, nil
}

// control serves the stack's own requests of the tests. Each POST
// /pending-agent starts one more agent pairing-N that waits for approval;
// the calls run one after another, as each switches pairing by
// fingerprint on and off again.
func control(ctx context.Context, panel, o, agentAddr, root string) http.Handler {
	var mu sync.Mutex
	n := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pending-agent", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		n++
		name := fmt.Sprintf("pairing-%d", n)
		admin := &client{base: panel, origin: o}
		err := admin.call(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "admin", "password": adminPassword}, nil)
		if err == nil {
			err = pendingAgent(ctx, admin, agentAddr, root, name)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": name})
	})
	return mux
}
