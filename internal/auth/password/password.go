// SPDX-License-Identifier: AGPL-3.0-or-later

// Package password hashes and verifies local passwords with Argon2id
// (AUTH-001, Design 4.1). Hashes use the PHC string format, so the
// parameters travel with each hash and can be raised later.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Length limits of a password in characters.
const (
	MinLength = 12
	MaxLength = 128
)

// Params are the Argon2id cost parameters.
type Params struct {
	Time      uint32
	MemoryKiB uint32
	Threads   uint8
	SaltLen   int
	KeyLen    uint32
}

// Default follows the second recommended option of RFC 9106: t=3, 64 MiB,
// p=4.
var Default = Params{Time: 3, MemoryKiB: 64 * 1024, Threads: 4, SaltLen: 16, KeyLen: 32}

// DefaultWait is how long a request waits for a free hashing slot.
const DefaultWait = 5 * time.Second

var (
	// ErrBusy means no hashing slot became free in time; the API answers
	// 503.
	ErrBusy = errors.New("password hashing is busy")
	// ErrLength means the password is shorter than MinLength or longer
	// than MaxLength characters.
	ErrLength = fmt.Errorf("password must have %d to %d characters", MinLength, MaxLength)
	errFormat = errors.New("unsupported password hash")
)

// Hasher computes hashes with bounded parallelism: each computation takes
// MemoryKiB of RAM, so only a few may run at once.
type Hasher struct {
	params Params
	slots  chan struct{}
	wait   time.Duration
	dummy  string
}

// NewHasher returns a hasher that runs at most concurrency computations at
// a time and waits up to wait for a slot.
func NewHasher(p Params, concurrency int, wait time.Duration) (*Hasher, error) {
	if concurrency < 1 {
		return nil, errors.New("hash concurrency must be at least 1")
	}
	h := &Hasher{params: p, slots: make(chan struct{}, concurrency), wait: wait}
	dummy, err := h.compute(randomString())
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

// Validate checks the length rules of a new password.
func Validate(pw string) error {
	if n := utf8.RuneCountInString(pw); n < MinLength || n > MaxLength {
		return ErrLength
	}
	return nil
}

// Hash returns the PHC string of a new password.
func (h *Hasher) Hash(ctx context.Context, pw string) (string, error) {
	release, err := h.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return h.compute(pw)
}

// Verify reports whether pw matches encoded.
func (h *Hasher) Verify(ctx context.Context, encoded, pw string) (bool, error) {
	release, err := h.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return verify(encoded, pw)
}

// VerifyDummy costs as much as Verify and always fails. Logins of unknown
// users use it, so their answer takes the same time.
func (h *Hasher) VerifyDummy(ctx context.Context, pw string) error {
	_, err := h.Verify(ctx, h.dummy, pw)
	return err
}

func (h *Hasher) acquire(ctx context.Context) (func(), error) {
	t := time.NewTimer(h.wait)
	defer t.Stop()
	select {
	case h.slots <- struct{}{}:
		return func() { <-h.slots }, nil
	case <-t.C:
		return nil, ErrBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *Hasher) compute(pw string) (string, error) {
	p := h.params
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, p.KeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func verify(encoded, pw string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errFormat
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errFormat
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return false, errFormat
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, errFormat
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errFormat
	}
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want))) //nolint:gosec // length of a decoded hash
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func randomString() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawStdEncoding.EncodeToString(b)
}
