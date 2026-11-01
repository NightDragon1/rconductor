// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config reads the start-up configuration from environment variables
// with the prefix RCD_ (Design 5, D-031, D-033). Settings that change at
// runtime live in the database, not here.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Lookup returns the value of an environment variable and whether it is set.
// os.LookupEnv satisfies it; tests pass a map-backed function.
type Lookup func(key string) (string, bool)

// ReadFile reads the content of a file referenced by a _FILE variable.
type ReadFile func(name string) ([]byte, error)

// ErrNotAvailable marks a value that is valid in the concept but not yet
// supported by this release.
var ErrNotAvailable = errors.New("not available in this release")

// Log holds the logging settings shared by panel and agent.
type Log struct {
	Level  slog.Level
	Format string // "text" or "json"
}

// Panel is the start-up configuration of the panel (Design 5.2).
type Panel struct {
	HTTPAddr        string
	AgentAddr       string
	AgentPublicAddr string
	PublicURL       *url.URL // nil: derived from the request
	BasePath        string   // starts with "/", no trailing slash except for "/"
	TrustedProxies  []netip.Prefix
	DBDriver        string
	DBURL           string // secret, never log
	DataDir         string
	SecretsDir      string
	BackupDir       string
	MasterKeyFile   string // empty: generated in SecretsDir
	EmergencyAdmin  bool
	Log             Log
}

// Agent is the start-up configuration of the agent. Until M3 only the
// logging settings are read.
type Agent struct {
	Log Log
}

// LoadPanelFromOS loads the panel configuration from the process environment.
func LoadPanelFromOS() (Panel, error) { return LoadPanel(os.LookupEnv, os.ReadFile) }

// LoadAgentFromOS loads the agent configuration from the process environment.
func LoadAgentFromOS() (Agent, error) { return LoadAgent(os.LookupEnv, os.ReadFile) }

// LoadPanel reads and validates the panel configuration. All problems are
// reported together.
func LoadPanel(env Lookup, readFile ReadFile) (Panel, error) {
	r := reader{env: env, readFile: readFile}
	c := Panel{
		HTTPAddr:        r.addr("RCD_HTTP_ADDR", ":8080"),
		AgentAddr:       r.addr("RCD_AGENT_ADDR", ":8443"),
		AgentPublicAddr: r.str("RCD_AGENT_PUBLIC_ADDR", ""),
		PublicURL:       r.publicURL("RCD_PUBLIC_URL"),
		BasePath:        r.basePath("RCD_BASE_PATH", "/"),
		TrustedProxies:  r.prefixes("RCD_TRUSTED_PROXIES"),
		DBDriver:        r.dbDriver("RCD_DB_DRIVER", "sqlite"),
		DBURL:           r.secret("RCD_DB_URL"),
		DataDir:         r.str("RCD_DATA_DIR", "/data"),
		SecretsDir:      r.str("RCD_SECRETS_DIR", "/secrets"),
		BackupDir:       r.str("RCD_BACKUP_DIR", "/backups"),
		MasterKeyFile:   r.str("RCD_MASTER_KEY_FILE", ""),
		EmergencyAdmin:  r.boolean("RCD_EMERGENCY_ADMIN", true),
		Log:             r.log(),
	}
	if c.AgentPublicAddr != "" {
		if _, _, err := net.SplitHostPort(c.AgentPublicAddr); err != nil {
			r.fail("RCD_AGENT_PUBLIC_ADDR", "must be host:port")
		}
	}
	return c, r.err()
}

// LoadAgent reads and validates the agent configuration.
func LoadAgent(env Lookup, readFile ReadFile) (Agent, error) {
	r := reader{env: env, readFile: readFile}
	c := Agent{Log: r.log()}
	return c, r.err()
}

type reader struct {
	env      Lookup
	readFile ReadFile
	errs     []error
}

func (r *reader) fail(key, msg string) {
	r.errs = append(r.errs, fmt.Errorf("%s: %s", key, msg))
}

func (r *reader) err() error { return errors.Join(r.errs...) }

func (r *reader) str(key, def string) string {
	v, ok := r.env(key)
	v = strings.TrimSpace(v)
	if !ok || v == "" {
		return def
	}
	return v
}

// secret reads key or key_FILE (D-033). Setting both is an error. The value is
// never part of an error message.
func (r *reader) secret(key string) string {
	v, hasValue := r.env(key)
	path, hasFile := r.env(key + "_FILE")
	hasValue = hasValue && v != ""
	path = strings.TrimSpace(path)
	hasFile = hasFile && path != ""
	switch {
	case hasValue && hasFile:
		r.fail(key, "set either "+key+" or "+key+"_FILE, not both")
		return ""
	case hasFile:
		b, err := r.readFile(path)
		if err != nil {
			r.fail(key+"_FILE", "cannot read file")
			return ""
		}
		return strings.TrimRight(string(b), "\r\n")
	default:
		return v
	}
}

func (r *reader) addr(key, def string) string {
	v := r.str(key, def)
	if _, port, err := net.SplitHostPort(v); err != nil || port == "" {
		r.fail(key, "must be [host]:port")
	}
	return v
}

func (r *reader) boolean(key string, def bool) bool {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.fail(key, "must be true or false")
		return def
	}
	return b
}

func (r *reader) publicURL(key string) *url.URL {
	v := r.str(key, "")
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.fail(key, "must be an absolute http or https URL")
		return nil
	}
	return u
}

func (r *reader) basePath(key, def string) string {
	v := r.str(key, def)
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, "?#") {
		r.fail(key, "must be an absolute path such as /rcon")
		return def
	}
	if v = strings.TrimRight(v, "/"); v == "" {
		v = "/"
	}
	return v
}

func (r *reader) prefixes(key string) []netip.Prefix {
	v := r.str(key, "")
	if v == "" {
		return nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(part); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		r.fail(key, fmt.Sprintf("invalid IP or CIDR %q", part))
	}
	return out
}

func (r *reader) dbDriver(key, def string) string {
	v := strings.ToLower(r.str(key, def))
	switch v {
	case "sqlite":
	case "postgres":
		r.errs = append(r.errs, fmt.Errorf("%s: postgres: %w (planned for R2)", key, ErrNotAvailable))
	default:
		r.fail(key, "must be sqlite or postgres")
	}
	return v
}

func (r *reader) log() Log {
	l := Log{Level: slog.LevelInfo, Format: "text"}
	switch strings.ToLower(r.str("RCD_LOG_LEVEL", "info")) {
	case "debug":
		l.Level = slog.LevelDebug
	case "info":
	case "warn":
		l.Level = slog.LevelWarn
	case "error":
		l.Level = slog.LevelError
	default:
		r.fail("RCD_LOG_LEVEL", "must be debug, info, warn or error")
	}
	switch v := strings.ToLower(r.str("RCD_LOG_FORMAT", "text")); v {
	case "text", "json":
		l.Format = v
	default:
		r.fail("RCD_LOG_FORMAT", "must be text or json")
	}
	return l
}
