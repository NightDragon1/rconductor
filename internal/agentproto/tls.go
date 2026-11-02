// SPDX-License-Identifier: AGPL-3.0-or-later

package agentproto

import (
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/ops/keys"
)

// Path is the WebSocket path on the agent port.
const Path = "/agent/v1"

// ErrPinMismatch means the other side shows another key than the pinned
// one (AGT-012, agent.rejected with reason pin_mismatch).
var ErrPinMismatch = errors.New("the peer's key does not match the pinned key")

// PeerKey returns the Ed25519 key the other side showed in a finished
// handshake.
func PeerKey(cs tls.ConnectionState) (ed25519.PublicKey, error) {
	if len(cs.PeerCertificates) != 1 {
		return nil, fmt.Errorf("expected one peer certificate, got %d", len(cs.PeerCertificates))
	}
	pub, ok := cs.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("certificate without Ed25519 key")
	}
	return pub, nil
}

// ServerConfig is the TLS 1.3 configuration of the panel's agent port: it
// shows the panel key and requires an Ed25519 key from every agent. Which
// agent keys count is decided after the handshake, because an agent that
// pairs is not known yet (Design 1.2).
func ServerConfig(id *keys.Identity) (*tls.Config, error) {
	cert, err := id.Certificate("rconductor-panel")
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		// No resumption: every connection shows and checks its key.
		SessionTicketsDisabled: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			_, err := PeerKey(cs)
			return err
		},
	}, nil
}

// ClientConfig is the TLS 1.3 configuration of an agent: it shows the
// agent key and accepts only a panel whose key has the pinned fingerprint
// (AGT-012). An empty fingerprint accepts any panel key once, for pairing
// by fingerprint (way B, trust on first use); the agent then pins the key
// it saw.
func ClientConfig(id *keys.Identity, panelFingerprint string) (*tls.Config, error) {
	cert, err := id.Certificate("rconductor-agent")
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		// The chain is not checked against a CA; VerifyConnection pins the
		// key instead (D-005). It also runs on resumed sessions.
		InsecureSkipVerify: true, //nolint:gosec // pinned below
		VerifyConnection: func(cs tls.ConnectionState) error {
			pub, err := PeerKey(cs)
			if err != nil {
				return err
			}
			if panelFingerprint != "" && keys.Fingerprint(pub) != panelFingerprint {
				return ErrPinMismatch
			}
			return nil
		},
	}, nil
}
