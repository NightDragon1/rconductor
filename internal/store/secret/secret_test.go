// SPDX-License-Identifier: AGPL-3.0-or-later

package secret

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
)

const password = "rcon-password-Xy7!"

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

var here = Context{Table: "servers", ID: "s1", Column: "secret"}

func TestSealOpen(t *testing.T) {
	b, err := New(key(t))
	if err != nil {
		t.Fatal(err)
	}
	v1, err := b.Seal(password, here)
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := b.Seal(password, here)
	if bytes.Equal(v1, v2) {
		t.Error("same ciphertext twice; nonce not random")
	}
	if bytes.Contains(v1, []byte(password)) {
		t.Error("plaintext in the stored value")
	}
	if id, _ := KeyIDOf(v1); id != b.KeyID() || len(id) != 16 {
		t.Errorf("key ID %q", id)
	}
	got, err := b.Open(v1, here)
	if err != nil || got.Reveal() != password {
		t.Fatalf("Open = %q, %v", got.Reveal(), err)
	}
}

func TestBoundToRecord(t *testing.T) {
	b, _ := New(key(t))
	v, _ := b.Seal(password, here)
	for _, c := range []Context{
		{Table: "servers", ID: "s2", Column: "secret"},
		{Table: "servers", ID: "s1", Column: "other"},
		{Table: "agents", ID: "s1", Column: "secret"},
	} {
		if _, err := b.Open(v, c); !errors.Is(err, ErrCorrupt) {
			t.Errorf("value opened for %+v: %v", c, err)
		}
	}
	v[len(v)-1] ^= 1
	if _, err := b.Open(v, here); !errors.Is(err, ErrCorrupt) {
		t.Errorf("tampered value: %v", err)
	}
	for _, bad := range [][]byte{nil, {9}, {1, 40, 'a'}} {
		if _, err := b.Open(bad, here); !errors.Is(err, ErrCorrupt) {
			t.Errorf("malformed %v: %v", bad, err)
		}
	}
}

// TestOtherMasterKey covers SRV-002: after the master key is lost and
// created anew, old values are recognized as sealed with another key.
func TestOtherMasterKey(t *testing.T) {
	old, _ := New(key(t))
	v, _ := old.Seal(password, here)
	fresh, _ := New(key(t))
	if old.KeyID() == fresh.KeyID() {
		t.Fatal("two keys with the same ID")
	}
	if _, err := fresh.Open(v, here); !errors.Is(err, ErrOtherKey) {
		t.Errorf("Open with another key: %v", err)
	}
	if _, err := New(make([]byte, 16)); err == nil {
		t.Error("short master key accepted")
	}
}

// TestPlaintextNeverLeaks: the decrypted credentials never show in a log
// line, fmt output, JSON or an error message.
func TestPlaintextNeverLeaks(t *testing.T) {
	b, _ := New(key(t))
	v, _ := b.Seal(password, here)
	got, err := b.Open(v, here)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	for _, format := range []string{"json", "text"} {
		log := logging.New(&logs, "panel", config.Log{Level: slog.LevelDebug, Format: format}).Root()
		log.Debug("opened", slog.Any("value", got), slog.String("rcon_password", got.Reveal()), slog.Any("ctx", here))
	}
	js, _ := json.Marshal(map[string]any{"secret": got})
	_, errOpen := b.Open(v, Context{Table: "servers", ID: "x", Column: "secret"})
	for name, out := range map[string]string{
		"log":   logs.String(),
		"fmt":   fmt.Sprintf("%v %s %+v %#v", got, got, got, got),
		"json":  string(js),
		"error": errOpen.Error(),
	} {
		if bytes.Contains([]byte(out), []byte(password)) {
			t.Errorf("plaintext in %s: %s", name, out)
		}
	}
}
