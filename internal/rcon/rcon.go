// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rcon defines the protocol adapters through which the panel talks
// to game servers (PROT-001, Architecture 7: extension point). Adapters
// for further protocols (HTTP API in 1.1, later BattlEye, WebRCON, Telnet)
// implement the same interfaces.
package rcon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
)

var (
	// ErrAuth means the game server refused the credentials (SRV-004).
	ErrAuth = errors.New("rcon authentication failed")
	// ErrTooLarge means a packet or response exceeded the limits for
	// foreign data (SEC-006).
	ErrTooLarge = errors.New("rcon response too large")
	// ErrCertMismatch means the certificate of the game server is not the
	// pinned one; nothing was sent (PROT-003, D-071).
	ErrCertMismatch = errors.New("the certificate of the game server is not the trusted one")
	// ErrUnpinned means no certificate is trusted yet; nothing was sent.
	ErrUnpinned = errors.New("no certificate of the game server is trusted yet")
	// ErrCommandFailed means the game ran the command and reported it as
	// failed (D-072, D-073); the answer text comes with it. It is a result
	// of the command, not a broken connection.
	ErrCommandFailed = errors.New("the game reported the command as failed")
)

// Endpoint is what an adapter needs beyond address and password: the
// pinned certificate, the dialect of the game and the parallel requests;
// for Source RCON the end marker of the game profile (PROT-001), empty
// for the default.
type Endpoint struct {
	Addr        string
	Secret      logging.Secret
	Fingerprint string
	Dialect     string
	Parallel    int
	EndMarker   string
}

// EndpointDialer is implemented by adapters that need an Endpoint; the
// connection manager prefers it to Dial.
type EndpointDialer interface {
	DialEndpoint(ctx context.Context, e Endpoint) (Conn, error)
}

// Certificate is the leaf certificate a game server showed: its SHA-256
// fingerprint and its end of validity (PROT-003, D-072). The pin is the
// fingerprint only; NotAfter is shown, never checked.
type Certificate struct {
	Fingerprint string
	NotAfter    time.Time
}

// FingerprintReader is implemented by adapters with a pinned certificate:
// it reads the certificate of addr in a handshake that sends nothing
// else, for the connection test before anything is trusted.
type FingerprintReader interface {
	Certificate(ctx context.Context, addr string) (Certificate, error)
}

// Players are the players online as a game reports them: the count, the
// limit (0 if unknown) and the names, empty if the game gives none.
type Players struct {
	Count, Max int
	Names      []string
}

// PlayerCounter is implemented by connections that ask the game for its
// players without a console command (protocol http-api, PROT-003).
type PlayerCounter interface {
	Players(ctx context.Context) (Players, error)
}

// PinnedConn is implemented by connections with a pinned certificate: the
// certificate of the last handshake, which matched the pin (D-073).
type PinnedConn interface {
	Certificate() (Certificate, bool)
}

// Concurrent is implemented by connections that take several commands at
// once; the connection manager then does not serialize them (PROT-002
// holds for RCON only).
type Concurrent interface {
	Concurrent()
}

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

// Prober is implemented by connections that can tell, without sending a
// command, whether the server still holds them; the connection manager
// uses it to notice a stopped game server while idle (SRV-004).
type Prober interface {
	Alive() error
}

// Pinger is implemented by connections that can send a probe the server
// must answer; the connection manager uses it while idle, because a
// half-open connection after a host crash or network loss gives no sign
// to a read (SRV-004, Design 3.8).
type Pinger interface {
	Ping(ctx context.Context) error
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
