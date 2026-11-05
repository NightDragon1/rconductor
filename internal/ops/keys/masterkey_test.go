// SPDX-License-Identifier: AGPL-3.0-or-later

package keys

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/ops/backup"
	"github.com/NightDragon1/rconductor/internal/store"
)

func newLogger(buf *bytes.Buffer) *slog.Logger {
	return logging.New(buf, "panel", config.Log{Level: slog.LevelDebug, Format: "json"}).Module("keys")
}

func TestCreateThenLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	var logs bytes.Buffer

	k1, err := LoadOrCreate(newLogger(&logs), Options{SecretsDir: dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(k1.Bytes()) != Size || k1.Path != filepath.Join(dir, FileName) {
		t.Fatalf("key = %d bytes at %s", len(k1.Bytes()), k1.Path)
	}
	logtest.Wait(t, &logs, `"event":"key.created"`)

	k2, err := LoadOrCreate(newLogger(&logs), Options{SecretsDir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !bytes.Equal(k1.Bytes(), k2.Bytes()) {
		t.Error("reloaded key differs")
	}
	logtest.Wait(t, &logs, `"event":"key.loaded"`)

	assertNoKey(t, logs.Bytes(), k1.Bytes(), "log")
	if s := fmt.Sprintf("%v %s", k1, k1); strings.Contains(s, base64.StdEncoding.EncodeToString(k1.Bytes())) {
		t.Error("fmt reveals key")
	}
}

func TestExternalKeyFile(t *testing.T) {
	dir := t.TempDir()
	want := bytes.Repeat([]byte{0x42}, Size)
	ext := filepath.Join(dir, "docker-secret")
	if err := os.WriteFile(ext, []byte(base64.StdEncoding.EncodeToString(want)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	k, err := LoadOrCreate(newLogger(&logs), Options{SecretsDir: filepath.Join(dir, "secrets"), File: ext})
	if err != nil {
		t.Fatalf("load external: %v", err)
	}
	if !bytes.Equal(k.Bytes(), want) || k.Path != ext {
		t.Error("external key not used")
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets", FileName)); !os.IsNotExist(err) {
		t.Error("a key was generated although RCD_MASTER_KEY_FILE is set")
	}
}

func TestExternalKeyFileMissingIsError(t *testing.T) {
	var logs bytes.Buffer
	_, err := LoadOrCreate(newLogger(&logs), Options{SecretsDir: t.TempDir(), File: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Fatal("missing external key must fail, not generate a new key")
	}
}

func TestInvalidKeyFile(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"short":  base64.StdEncoding.EncodeToString([]byte("too short")),
		"binary": "\x00\x01not base64",
	} {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		var logs bytes.Buffer
		if _, err := LoadOrCreate(newLogger(&logs), Options{SecretsDir: dir}); err == nil {
			t.Errorf("%s: invalid key accepted", name)
		}
	}
}

func TestKeyNotInDataOrBackupDir(t *testing.T) {
	root := t.TempDir()
	data, backups := filepath.Join(root, "data"), filepath.Join(root, "backups")
	var logs bytes.Buffer
	for _, dir := range []string{data, filepath.Join(backups, "sub")} {
		_, err := LoadOrCreate(newLogger(&logs), Options{SecretsDir: dir, Forbidden: []string{data, backups}})
		if err == nil {
			t.Errorf("key accepted inside %s", dir)
		}
	}
	if _, err := LoadOrCreate(newLogger(&logs), Options{
		SecretsDir: filepath.Join(root, "secrets"), Forbidden: []string{data, backups},
	}); err != nil {
		t.Errorf("key in separate secrets dir refused: %v", err)
	}
	// A sibling whose name only starts with the data dir is fine.
	if _, err := LoadOrCreate(newLogger(&logs), Options{
		SecretsDir: data + "-secrets", Forbidden: []string{data},
	}); err != nil {
		t.Errorf("sibling dir refused: %v", err)
	}
}

// TestKeyNeitherInDatabaseNorBackup covers SEC-005: after a normal start,
// neither the database files nor a backup contain the key.
func TestKeyNeitherInDatabaseNorBackup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	data, secrets, backups := filepath.Join(root, "data"), filepath.Join(root, "secrets"), filepath.Join(root, "backups")
	var logs bytes.Buffer
	log := newLogger(&logs)

	k, err := LoadOrCreate(log, Options{SecretsDir: secrets, Forbidden: []string{data, backups}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Migrate(ctx, log, store.MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backup.EnsureDefaults(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.New(s.DB, backups, log).Run(ctx, backup.KindAuto, 0, backup.DefaultPolicy()); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{data, backups} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			assertNoKey(t, b, k.Bytes(), filepath.Join(filepath.Base(dir), e.Name()))
		}
	}
	assertNoKey(t, logs.Bytes(), k.Bytes(), "log")
}

func assertNoKey(t *testing.T, haystack, key []byte, where string) {
	t.Helper()
	if bytes.Contains(haystack, key) || bytes.Contains(haystack, []byte(base64.StdEncoding.EncodeToString(key))) {
		t.Errorf("master key found in %s", where)
	}
}
