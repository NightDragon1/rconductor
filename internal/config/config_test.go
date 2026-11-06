// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
)

func envOf(m map[string]string) Lookup {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func filesOf(m map[string]string) ReadFile {
	return func(name string) ([]byte, error) {
		v, ok := m[name]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(v), nil
	}
}

func TestLoadPanelDefaults(t *testing.T) {
	c, err := LoadPanel(envOf(nil), filesOf(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.HTTPAddr != ":8080" || c.AgentAddr != ":8443" {
		t.Errorf("ports = %q, %q; want :8080, :8443", c.HTTPAddr, c.AgentAddr)
	}
	if c.DataDir != "/data" || c.SecretsDir != "/secrets" || c.BackupDir != "/backups" {
		t.Errorf("dirs = %q %q %q", c.DataDir, c.SecretsDir, c.BackupDir)
	}
	if c.DBDriver != "sqlite" || c.BasePath != "/" || !c.EmergencyAdmin {
		t.Errorf("driver=%q base=%q emergency=%v", c.DBDriver, c.BasePath, c.EmergencyAdmin)
	}
	if c.PublicURL != nil || c.TrustedProxies != nil || c.MasterKeyFile != "" || c.AgentPublicAddr != "" {
		t.Errorf("expected empty optional values, got %+v", c)
	}
	if c.Log.Level != slog.LevelInfo || c.Log.Format != "text" {
		t.Errorf("log = %+v", c.Log)
	}
	if c.HashConcurrency != 2 {
		t.Errorf("hash concurrency = %d, want 2", c.HashConcurrency)
	}
	if c.MaxConnections != 512 {
		t.Errorf("max connections = %d, want 512", c.MaxConnections)
	}
}

func TestLoadPanelValues(t *testing.T) {
	c, err := LoadPanel(envOf(map[string]string{
		"RCD_HTTP_ADDR":         "127.0.0.1:9000",
		"RCD_AGENT_PUBLIC_ADDR": "panel.example.net:8443",
		"RCD_PUBLIC_URL":        "https://rcon.example.net/rcon",
		"RCD_BASE_PATH":         "/rcon/",
		"RCD_TRUSTED_PROXIES":   "10.0.0.0/8, 192.168.1.5,fd00::/8",
		"RCD_EMERGENCY_ADMIN":   "false",
		"RCD_MASTER_KEY_FILE":   "/run/secrets/master",
		"RCD_LOG_LEVEL":         "DEBUG",
		"RCD_LOG_FORMAT":        "json",

		"RCD_PASSWORD_HASH_CONCURRENCY": "4",
		"RCD_MAX_CONNECTIONS":           "2000",
	}), filesOf(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BasePath != "/rcon" {
		t.Errorf("BasePath = %q, want /rcon", c.BasePath)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.5/32"),
		netip.MustParsePrefix("fd00::/8"),
	}
	if len(c.TrustedProxies) != len(want) {
		t.Fatalf("TrustedProxies = %v", c.TrustedProxies)
	}
	for i := range want {
		if c.TrustedProxies[i] != want[i] {
			t.Errorf("TrustedProxies[%d] = %v, want %v", i, c.TrustedProxies[i], want[i])
		}
	}
	if c.EmergencyAdmin || c.MasterKeyFile != "/run/secrets/master" || c.PublicURL.Host != "rcon.example.net" {
		t.Errorf("unexpected config %+v", c)
	}
	if c.Log.Level != slog.LevelDebug || c.Log.Format != "json" {
		t.Errorf("log = %+v", c.Log)
	}
	if c.HashConcurrency != 4 {
		t.Errorf("hash concurrency = %d, want 4", c.HashConcurrency)
	}
	if c.MaxConnections != 2000 {
		t.Errorf("max connections = %d, want 2000", c.MaxConnections)
	}
}

func TestSecretFromFile(t *testing.T) {
	c, err := LoadPanel(
		envOf(map[string]string{"RCD_DB_URL_FILE": "/run/secrets/db"}),
		filesOf(map[string]string{"/run/secrets/db": "postgres://u:hunter2@db/x\n"}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.DBURL != "postgres://u:hunter2@db/x" {
		t.Errorf("DBURL not read from file")
	}
}

func TestSecretBothSet(t *testing.T) {
	_, err := LoadPanel(envOf(map[string]string{
		"RCD_DB_URL":      "postgres://u:hunter2@db/x",
		"RCD_DB_URL_FILE": "/run/secrets/db",
	}), filesOf(nil))
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("want error for both set, got %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks secret: %v", err)
	}
}

func TestSecretFileMissing(t *testing.T) {
	_, err := LoadPanel(envOf(map[string]string{"RCD_DB_URL_FILE": "/nope"}), filesOf(nil))
	if err == nil || !strings.Contains(err.Error(), "RCD_DB_URL_FILE") {
		t.Fatalf("want file error, got %v", err)
	}
}

func TestPostgresNotAvailable(t *testing.T) {
	_, err := LoadPanel(envOf(map[string]string{"RCD_DB_DRIVER": "postgres"}), filesOf(nil))
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("want ErrNotAvailable, got %v", err)
	}
}

func TestInvalidValuesReportedTogether(t *testing.T) {
	_, err := LoadPanel(envOf(map[string]string{
		"RCD_HTTP_ADDR":         "8080",
		"RCD_AGENT_PUBLIC_ADDR": "panel.example.net",
		"RCD_BASE_PATH":         "rcon",
		"RCD_TRUSTED_PROXIES":   "10.0.0.0/8,not-an-ip",
		"RCD_DB_DRIVER":         "mysql",
		"RCD_EMERGENCY_ADMIN":   "maybe",
		"RCD_PUBLIC_URL":        "ftp://x",
		"RCD_LOG_LEVEL":         "loud",
		"RCD_LOG_FORMAT":        "xml",

		"RCD_PASSWORD_HASH_CONCURRENCY": "0",
		"RCD_MAX_CONNECTIONS":           "15",
	}), filesOf(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for _, key := range []string{
		"RCD_HTTP_ADDR", "RCD_AGENT_PUBLIC_ADDR", "RCD_BASE_PATH", "RCD_TRUSTED_PROXIES",
		"RCD_DB_DRIVER", "RCD_EMERGENCY_ADMIN", "RCD_PUBLIC_URL", "RCD_LOG_LEVEL", "RCD_LOG_FORMAT",
		"RCD_PASSWORD_HASH_CONCURRENCY",
		"RCD_MAX_CONNECTIONS",
	} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestLoadAgent(t *testing.T) {
	c, err := LoadAgent(envOf(map[string]string{"RCD_LOG_FORMAT": "json"}), filesOf(nil))
	if err != nil || c.Log.Format != "json" || c.LabelKey != "rcon.manage" || c.LabelValue != "true" ||
		c.DockerHost != "unix:///var/run/docker.sock" || c.SecretsDir != "/secrets" || c.PairingToken != "" {
		t.Fatalf("defaults: %+v, %v", c, err)
	}
	// The token also comes from a file (D-033, revision 2.3 candidate 4).
	c, err = LoadAgent(envOf(map[string]string{"RCD_PAIRING_TOKEN_FILE": "/run/secrets/token", "RCD_LABEL": "game=yes", "RCD_PANEL_ADDR": "panel:8443"}),
		filesOf(map[string]string{"/run/secrets/token": "rcd1.abc\n"}))
	if err != nil || c.PairingToken != "rcd1.abc" || c.LabelKey != "game" || c.LabelValue != "yes" || c.PanelAddr != "panel:8443" {
		t.Fatalf("file token: %+v, %v", c, err)
	}
	for name, env := range map[string]map[string]string{
		"label":      {"RCD_LABEL": "novalue"},
		"panel addr": {"RCD_PANEL_ADDR": "panel"},
		"both":       {"RCD_PAIRING_TOKEN": "a", "RCD_PAIRING_TOKEN_FILE": "/x"},
	} {
		if _, err := LoadAgent(envOf(env), filesOf(map[string]string{"/x": "b"})); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
