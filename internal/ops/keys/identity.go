// SPDX-License-Identifier: AGPL-3.0-or-later

package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
)

// Identity file names in the secrets directory (Design 1.1, 1.2).
const (
	PanelKeyFile = "panel.key"
	AgentKeyFile = "agent.key"
)

// Identity is the Ed25519 key pair of the panel or an agent; the other
// side pins its public key (D-005, AGT-012). It never prints the private
// key.
type Identity struct {
	priv ed25519.PrivateKey
	Path string
}

// Public returns the public key.
func (id *Identity) Public() ed25519.PublicKey { return id.priv.Public().(ed25519.PublicKey) }

// Fingerprint is the fingerprint of the public key, as people compare it.
func (id *Identity) Fingerprint() string { return Fingerprint(id.Public()) }

// String implements fmt.Stringer without revealing the key.
func (id *Identity) String() string { return "identity " + id.Fingerprint() }

// LogValue implements slog.LogValuer without revealing the key.
func (id *Identity) LogValue() slog.Value { return slog.StringValue(id.Fingerprint()) }

// Fingerprint returns SHA256:<base64> of a public key, the form OpenSSH
// uses.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Certificate returns a self-signed certificate for TLS; trust comes from
// pinning the key, not from the certificate (Design 1.1).
func (id *Identity) Certificate(name string) (tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{name},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, id.Public(), id.priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: id.priv}, nil
}

// LoadOrCreateIdentity reads the key at path or creates it on first start;
// it logs key.created or key.loaded with the fingerprint.
func LoadOrCreateIdentity(log *slog.Logger, path string) (*Identity, error) {
	priv, err := readIdentity(path)
	if err == nil {
		warnPermissions(log, path)
		id := &Identity{priv: priv, Path: path}
		log.Info("key pair loaded", logging.Event(logging.KeyLoaded), slog.String("path", path), slog.String("fingerprint", id.Fingerprint()))
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, priv, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key pair: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path from configuration
	if errors.Is(err, os.ErrExist) {
		return LoadOrCreateIdentity(log, path)
	}
	if err != nil {
		return nil, fmt.Errorf("create key pair: %w", err)
	}
	werr := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write key pair: %w", werr)
	}
	id := &Identity{priv: priv, Path: path}
	log.Info("key pair created", logging.Event(logging.KeyCreated), slog.String("path", path), slog.String("fingerprint", id.Fingerprint()))
	return id, nil
}

func readIdentity(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path from configuration
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("key pair %s: %w", path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("read key pair %s: %w", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("key pair %s: not a PEM private key", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("key pair %s: %w", path, err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key pair %s: not an Ed25519 key", path)
	}
	return priv, nil
}
