// SPDX-License-Identifier: AGPL-3.0-or-later

// Package secret encrypts credentials stored in the database (SRV-002,
// Design 4.1, D-030): AES-256-GCM with a key per key ID derived from the
// master key by HKDF. Table, record ID and column are additional
// authenticated data, so a value cannot be moved to another record.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/logging"
)

// version of the stored format: version byte, key ID length, key ID,
// nonce, ciphertext with tag.
const version = 1

// keyIDLen is the length of a key ID in characters.
const keyIDLen = 16

var (
	// ErrOtherKey means the value was sealed with another master key, for
	// example after the key was lost and created anew; the credentials
	// must be entered again (SRV-002).
	ErrOtherKey = errors.New("credentials were encrypted with another master key")
	// ErrCorrupt means the value is damaged or belongs to another record.
	ErrCorrupt = errors.New("credentials cannot be decrypted")
)

// Context binds a value to its place in the database.
type Context struct {
	Table, ID, Column string
}

func (c Context) aad() []byte {
	return []byte(c.Table + "\x00" + c.ID + "\x00" + c.Column)
}

// Box seals and opens values with the current master key.
type Box struct {
	keyID string
	aead  cipher.AEAD
}

// KeyID names a master key without revealing it: 16 hex characters of a
// hash of the key.
func KeyID(master []byte) string {
	sum := sha256.Sum256(append([]byte("rconductor key id\x00"), master...))
	return hex.EncodeToString(sum[:keyIDLen/2])
}

// New derives the encryption key for the current master key.
func New(master []byte) (*Box, error) {
	if len(master) != 32 {
		return nil, errors.New("master key must have 32 bytes")
	}
	id := KeyID(master)
	key, err := hkdf.Key(sha256.New, master, nil, "rconductor secret v1 "+id, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{keyID: id, aead: aead}, nil
}

// KeyID returns the ID of the box's key.
func (b *Box) KeyID() string { return b.keyID }

// Seal encrypts plaintext for its place c.
func (b *Box) Seal(plaintext logging.Secret, c Context) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 2+len(b.keyID)+len(nonce)+len(plaintext)+b.aead.Overhead())
	out = append(out, version, keyIDLen)
	out = append(out, b.keyID...)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, []byte(plaintext.Reveal()), c.aad()), nil
}

// KeyIDOf returns the key ID a value was sealed with.
func KeyIDOf(value []byte) (string, error) {
	if len(value) < 2 || value[0] != version || len(value) < 2+int(value[1]) {
		return "", ErrCorrupt
	}
	return string(value[2 : 2+int(value[1])]), nil
}

// Open decrypts a value sealed for c.
func (b *Box) Open(value []byte, c Context) (logging.Secret, error) {
	id, err := KeyIDOf(value)
	if err != nil {
		return "", err
	}
	if id != b.keyID {
		return "", ErrOtherKey
	}
	rest := value[2+len(id):]
	n := b.aead.NonceSize()
	if len(rest) < n+b.aead.Overhead() {
		return "", ErrCorrupt
	}
	plain, err := b.aead.Open(nil, rest[:n], rest[n:], c.aad())
	if err != nil {
		return "", fmt.Errorf("%w: %s.%s", ErrCorrupt, c.Table, c.Column)
	}
	return logging.Secret(plain), nil
}
