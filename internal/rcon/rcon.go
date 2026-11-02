// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rcon defines the protocol adapters through which the panel talks
// to game servers (PROT-001, Architecture 7: extension point). Adapters
// for further protocols (HTTP API in R2, BattlEye, WebRCON, Telnet in R3)
// implement the same interfaces.
package rcon

import (
	"context"
	"errors"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/logging"
)

var (
	// ErrAuth means the game server refused the credentials (SRV-004).
	ErrAuth = errors.New("rcon authentication failed")
	// ErrTooLarge means a packet or response exceeded the limits for
	// foreign data (SEC-006).
	ErrTooLarge = errors.New("rcon response too large")
)

// Adapter opens connections of one protocol.
type Adapter interface {
	// Dial connects to addr (host:port) and authenticates.
	Dial(ctx context.Context, addr string, password logging.Secret) (Conn, error)
}

// Conn is one authenticated connection to a game server. Execute is
// called by one goroutine at a time; the connection manager serializes
// commands per server (PROT-002).
type Conn interface {
	// Execute sends one command and returns the complete response.
	Execute(ctx context.Context, command string) (string, error)
	Close() error
}

// Registry maps protocol names of the game profiles to adapters.
type Registry map[string]Adapter

// Get returns the adapter of a protocol.
func (r Registry) Get(protocol string) (Adapter, error) {
	a, ok := r[protocol]
	if !ok {
		return nil, fmt.Errorf("no adapter for protocol %q", protocol)
	}
	return a, nil
}
