// SPDX-License-Identifier: AGPL-3.0-or-later

package agentproto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
)

// tokenPrefix marks version 1 of the pairing token (revision 2.3
// candidate 4).
const tokenPrefix = "rcd1."

// PairingToken is what a pairing token (way A) carries: where the panel
// is, the fingerprint of its key and a one-time secret. The agent checks
// the fingerprint in the TLS handshake and sends the secret only then.
type PairingToken struct {
	Addr        string `json:"a"`
	Fingerprint string `json:"f"`
	Secret      string `json:"s"`
}

// ErrInvalidToken means the text is no pairing token.
var ErrInvalidToken = errors.New("not a valid pairing token")

// NewPairingToken creates a token with a fresh 32-byte secret.
func NewPairingToken(addr, fingerprint string) (PairingToken, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return PairingToken{}, err
	}
	return PairingToken{Addr: addr, Fingerprint: fingerprint, Secret: base64.RawURLEncoding.EncodeToString(b)}, nil
}

// Encode returns the token as the agent receives it, secret included.
func (t PairingToken) Encode() string {
	b, _ := json.Marshal(t) //nolint:gosec // the secret belongs in the token; it is shown once
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// String names the token without its secret, so that printing it by
// mistake reveals nothing (SEC-012).
func (t PairingToken) String() string { return "pairing token for " + t.Addr + " " + t.Fingerprint }

// LogValue implements slog.LogValuer without the secret.
func (t PairingToken) LogValue() slog.Value { return slog.StringValue(t.String()) }

// ParsePairingToken reads a token.
func ParsePairingToken(s string) (PairingToken, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, tokenPrefix) || len(s) > 1024 {
		return PairingToken{}, ErrInvalidToken
	}
	b, err := base64.RawURLEncoding.DecodeString(s[len(tokenPrefix):])
	if err != nil {
		return PairingToken{}, ErrInvalidToken
	}
	var t PairingToken
	if json.Unmarshal(b, &t) != nil || t.Fingerprint == "" || len(t.Secret) < 32 {
		return PairingToken{}, ErrInvalidToken
	}
	return t, nil
}

// SecretHash is the SHA-256 of a token secret, the only form the panel
// stores.
func SecretHash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
