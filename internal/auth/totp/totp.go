// SPDX-License-Identifier: AGPL-3.0-or-later

// Package totp is the time-based one-time password of RFC 6238 for the
// second factor (AUTH-008): HMAC-SHA1, 6 digits, 30 s steps, one step of
// clock skew either way. It needs no dependency; the comparison takes the
// same time whatever the code.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 and every authenticator app use HMAC-SHA1; it is not used as a hash of data
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Parameters of the codes; the authenticator apps assume them.
const (
	Digits      = 6
	Period      = 30 * time.Second
	Skew        = 1
	SecretBytes = 20
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a random secret of SecretBytes.
func NewSecret() ([]byte, error) {
	s := make([]byte, SecretBytes)
	_, err := rand.Read(s)
	return s, err
}

// Encode gives the secret as the apps want it typed in: base32 without
// padding.
func Encode(secret []byte) string { return b32.EncodeToString(secret) }

// URI is the otpauth URI of the secret for the QR code.
func URI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", Encode(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(Digits))
	q.Set("period", strconv.Itoa(int(Period/time.Second)))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Step is the time step of t.
func Step(t time.Time) int64 { return t.Unix() / int64(Period/time.Second) }

// Code is the code of a step (RFC 4226 dynamic truncation).
func Code(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // steps are positive
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	s := strconv.FormatUint(uint64(v%1_000_000), 10)
	return strings.Repeat("0", Digits-len(s)) + s
}

// Normalize removes spaces a user typed and reports whether the rest is a
// code of Digits digits.
func Normalize(code string) (string, bool) {
	c := strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(c) != Digits {
		return "", false
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return c, true
}

// Verify checks code against the steps around now and returns the step it
// matched. Every candidate is compared, in constant time, so that the time
// tells nothing about the code. The caller keeps a step from being used
// twice.
func Verify(secret []byte, code string, now time.Time) (int64, bool) {
	c, ok := Normalize(code)
	if !ok {
		c = strings.Repeat("x", Digits) // compare anyway, never matches
	}
	cur := Step(now)
	var found int64
	match := 0
	for d := int64(-Skew); d <= Skew; d++ {
		eq := subtle.ConstantTimeCompare([]byte(Code(secret, cur+d)), []byte(c))
		if eq == 1 && found == 0 {
			found = cur + d
		}
		match |= eq
	}
	return found, ok && match == 1
}
