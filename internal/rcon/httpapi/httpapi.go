// SPDX-License-Identifier: AGPL-3.0-or-later

// Package httpapi is the protocol http-api (PROT-003, D-071): game servers
// with an HTTPS API. The base here holds everything the games share: the
// transport (net/http, no redirects, TLS 1.2 and up), the pinned
// certificate, the token, the limits of an answer, the parallel requests
// and the errors. Each game is a small Dialect that only builds its
// requests and reads its answers; it never opens a connection, sets TLS
// options or sees the token.
//
// The token leaves the panel only to a server whose certificate matches
// the pinned fingerprint: without a pin nothing is sent, on a mismatch the
// handshake fails before any request.
package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
)

// Protocol is the name of the protocol in game profiles and servers.
const Protocol = "http-api"

// MaxResponse bounds an answer before it is decoded; a longer answer is an
// error, not cut (SEC-006).
const MaxResponse = 64 << 10

// DefaultParallel is the number of requests at once to one server when the
// endpoint names none (setting servers.http_parallel).
const DefaultParallel = 4

// Request is one request a dialect builds: a path below the server's
// address and a JSON body.
type Request struct {
	Path string
	Body []byte
}

// Response is the answer as the base read it, at most MaxResponse bytes.
type Response struct {
	Status int
	Body   []byte
}

// Client sends a dialect's requests: the base sets scheme, address,
// Authorization and Content-Type.
type Client interface {
	Do(ctx context.Context, r Request) (Response, error)
}

// Dialect is one game's API.
type Dialect interface {
	// Name is the value of http_api.dialect in a game profile.
	Name() string
	// Health checks that the server answers and accepts the token.
	Health(ctx context.Context, c Client) error
	// Execute runs a console command and returns its output; when the
	// game reports the command as failed, the output comes with
	// rcon.ErrCommandFailed.
	Execute(ctx context.Context, c Client, command string) (string, error)
	// Players returns the players online; Names is empty if the game
	// gives none, the interface then shows the count only.
	Players(ctx context.Context, c Client) (Players, error)
}

// Players are the players online: the count, the limit (0 if unknown) and
// the names, if the game gives them.
type Players = rcon.Players

// dialects are the games, fixed in the code; a profile only names one.
var dialects = map[string]Dialect{}

func register(d Dialect) { dialects[d.Name()] = d }

func init() { register(Satisfactory{}) }

// Known reports whether a dialect exists, for the profile check.
func Known(name string) bool { _, ok := dialects[name]; return ok }

// Names lists the dialects, sorted.
func Names() []string {
	out := make([]string, 0, len(dialects))
	for n := range dialects {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Fingerprint is the SHA-256 of a certificate in DER, lower-case hex.
func Fingerprint(cert []byte) string {
	sum := sha256.Sum256(cert)
	return hex.EncodeToString(sum[:])
}

// NormalizeFingerprint takes a fingerprint as an admin may paste it, with
// colons, spaces or upper case, and returns it as Fingerprint writes it;
// ok is false if it is no SHA-256.
func NormalizeFingerprint(s string) (string, bool) {
	s = strings.ToLower(strings.NewReplacer(":", "", " ", "", "-", "").Replace(strings.TrimSpace(s)))
	if len(s) != 2*sha256.Size {
		return "", false
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return s, true
}

// tlsConfig is the one place that turns off the verification of the
// certificate chain, and only together with VerifyConnection: it accepts
// exactly the leaf certificate with the pinned fingerprint. With an empty
// pin it accepts any certificate but reports it to seen; the caller then
// sends nothing over the connection.
func tlsConfig(pin string, seen func(rcon.Certificate)) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		//nolint:gosec // the chain is replaced by the pin in VerifyConnection (D-071)
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return rcon.ErrCertMismatch
			}
			leaf := cs.PeerCertificates[0]
			fp := Fingerprint(leaf.Raw)
			if pin != "" && fp != pin {
				return rcon.ErrCertMismatch
			}
			// The pin is the fingerprint only: an expired certificate with
			// the pinned fingerprint is accepted (D-072).
			if seen != nil {
				seen(rcon.Certificate{Fingerprint: fp, NotAfter: leaf.NotAfter.UTC()})
			}
			return nil
		},
	}
}

// Adapter opens connections of the protocol http-api.
type Adapter struct{}

var (
	_ rcon.Adapter           = Adapter{}
	_ rcon.EndpointDialer    = Adapter{}
	_ rcon.FingerprintReader = Adapter{}
)

// Dial without an Endpoint has no pin: it sends nothing.
func (Adapter) Dial(context.Context, string, logging.Secret) (rcon.Conn, error) {
	return nil, rcon.ErrUnpinned
}

// Certificate reads the certificate of addr in a TLS handshake and sends
// nothing else: no request, no token.
func (Adapter) Certificate(ctx context.Context, addr string) (rcon.Certificate, error) {
	var cert rcon.Certificate
	d := tls.Dialer{Config: tlsConfig("", func(c rcon.Certificate) { cert = c })}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return rcon.Certificate{}, err
	}
	_ = c.Close()
	return cert, nil
}

// DialEndpoint connects with the pinned certificate and checks the token
// with the dialect's health check.
func (Adapter) DialEndpoint(ctx context.Context, e rcon.Endpoint) (rcon.Conn, error) {
	d, ok := dialects[e.Dialect]
	if !ok {
		return nil, fmt.Errorf("no http-api dialect %q", e.Dialect)
	}
	if e.Fingerprint == "" {
		return nil, rcon.ErrUnpinned
	}
	parallel := e.Parallel
	if parallel < 1 {
		parallel = DefaultParallel
	}
	c := &Conn{dialect: d, slots: make(chan struct{}, parallel)}
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSClientConfig:     tlsConfig(e.Fingerprint, c.seen),
		TLSHandshakeTimeout: 5 * time.Second,
		MaxConnsPerHost:     parallel,
		MaxIdleConnsPerHost: parallel,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	c.client = &client{
		base:  "https://" + e.Addr,
		token: e.Secret,
		http: &http.Client{
			Transport: tr,
			// Never follow a redirect: it could carry the token
			// elsewhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect },
		},
	}
	if err := d.Health(ctx, c.client); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

var errRedirect = errors.New("the game server answered with a redirect")

// client is the base side of a Client.
type client struct {
	base  string
	token logging.Secret
	http  *http.Client
}

func (c *client) Do(ctx context.Context, r Request) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(c.token))
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, rcon.ErrCertMismatch) {
			return Response{}, rcon.ErrCertMismatch
		}
		if errors.Is(err, errRedirect) {
			return Response{}, errRedirect
		}
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Response{}, errRedirect
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponse+1))
	if err != nil {
		return Response{}, err
	}
	if len(body) > MaxResponse {
		return Response{}, rcon.ErrTooLarge
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Response{}, rcon.ErrAuth
	}
	return Response{Status: resp.StatusCode, Body: body}, nil
}

// Conn is a connection to one server: requests go out in parallel up to
// the limit of the endpoint.
type Conn struct {
	dialect Dialect
	client  *client
	slots   chan struct{}
	// cert is the certificate of the last handshake; it matched the pin.
	cert atomic.Pointer[rcon.Certificate]
}

var (
	_ rcon.Conn          = (*Conn)(nil)
	_ rcon.Pinger        = (*Conn)(nil)
	_ rcon.Concurrent    = (*Conn)(nil)
	_ rcon.PinnedConn    = (*Conn)(nil)
	_ rcon.PlayerCounter = (*Conn)(nil)
)

func (c *Conn) seen(cert rcon.Certificate) { c.cert.Store(&cert) }

// Certificate is the pinned certificate as the server last showed it.
func (c *Conn) Certificate() (rcon.Certificate, bool) {
	if p := c.cert.Load(); p != nil {
		return *p, true
	}
	return rcon.Certificate{}, false
}

// Concurrent marks the connection as taking several commands at once.
func (*Conn) Concurrent() {}

func (c *Conn) take(ctx context.Context) error {
	select {
	case c.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Conn) give() { <-c.slots }

// Execute runs a console command through the dialect.
func (c *Conn) Execute(ctx context.Context, command string) (string, error) {
	if err := c.take(ctx); err != nil {
		return "", err
	}
	defer c.give()
	return c.dialect.Execute(ctx, c.client, command)
}

// Ping is the dialect's health check, for the idle probe.
func (c *Conn) Ping(ctx context.Context) error {
	if err := c.take(ctx); err != nil {
		return err
	}
	defer c.give()
	return c.dialect.Health(ctx, c.client)
}

// Players returns the players online through the dialect.
func (c *Conn) Players(ctx context.Context) (Players, error) {
	if err := c.take(ctx); err != nil {
		return Players{}, err
	}
	defer c.give()
	return c.dialect.Players(ctx, c.client)
}

// Close drops the idle connections.
func (c *Conn) Close() error {
	c.client.http.CloseIdleConnections()
	return nil
}
