// SPDX-License-Identifier: AGPL-3.0-or-later

// Package keys manages the master key of the panel. The key is created on
// first start and lives only in the secrets directory, apart from the
// database and its backups (SEC-005, SRV-002, D-015).
package keys

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/NightDragon1/rconductor/internal/logging"
)

// Size is the length of the master key in bytes (AES-256).
const Size = 32

// FileName is the name of the generated key in the secrets directory.
const FileName = "master.key"

// MasterKey holds the key bytes. It never prints its content.
type MasterKey struct {
	b    []byte
	Path string
}

// Bytes returns the key material.
func (k *MasterKey) Bytes() []byte { return k.b }

// String implements fmt.Stringer without revealing the key.
func (k *MasterKey) String() string { return logging.Redacted }

// LogValue implements slog.LogValuer without revealing the key.
func (k *MasterKey) LogValue() slog.Value { return slog.StringValue(logging.Redacted) }

// Options say where the key is.
type Options struct {
	SecretsDir string
	// File is RCD_MASTER_KEY_FILE; if set, the key is read from there and
	// never generated (e.g. a Docker secret).
	File string
	// Forbidden are directories the key must not be in (data and backups).
	Forbidden []string
}

// LoadOrCreate returns the master key. Without Options.File it reads
// <SecretsDir>/master.key and creates it on first start. It logs
// key.created or key.loaded with the path, never the key.
func LoadOrCreate(log *slog.Logger, opts Options) (*MasterKey, error) {
	path := opts.File
	explicit := path != ""
	if !explicit {
		path = filepath.Join(opts.SecretsDir, FileName)
	}
	if err := checkLocation(path, opts.Forbidden); err != nil {
		return nil, err
	}

	b, err := read(path)
	switch {
	case err == nil:
		warnPermissions(log, path)
		log.Info("master key loaded", logging.Event(logging.KeyLoaded),
			slog.String("path", path), slog.Bool("external", explicit))
		return &MasterKey{b: b, Path: path}, nil
	case explicit || !errors.Is(err, os.ErrNotExist):
		return nil, err
	}

	b, err = create(path)
	if errors.Is(err, os.ErrExist) {
		// Another start created it in the meantime.
		return LoadOrCreate(log, opts)
	}
	if err != nil {
		return nil, err
	}
	log.Info("master key created", logging.Event(logging.KeyCreated), slog.String("path", path))
	return &MasterKey{b: b, Path: path}, nil
}

func read(path string) ([]byte, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path comes from the operator's configuration
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("master key %s: %w", path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("read master key %s: %w", path, err)
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(b) != Size {
		return nil, fmt.Errorf("master key %s: must be %d bytes, base64 encoded (e.g. openssl rand -base64 32)", path, Size)
	}
	return b, nil
}

func create(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
	}
	b := make([]byte, Size)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path from configuration
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	_, werr := f.WriteString(base64.StdEncoding.EncodeToString(b) + "\n")
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write master key: %w", werr)
	}
	return b, nil
}

// checkLocation refuses a key inside the data or backup directory, so that
// neither the database volume nor a backup can carry it (SEC-005).
func checkLocation(path string, forbidden []string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("master key path: %w", err)
	}
	for _, dir := range forbidden {
		if dir == "" {
			continue
		}
		d, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(d, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("master key %s must not be inside %s", path, dir)
		}
	}
	return nil
}
