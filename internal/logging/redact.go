// SPDX-License-Identifier: AGPL-3.0-or-later

package logging

import (
	"log/slog"
	"regexp"
	"strings"
)

// Redacted replaces every secret value in the output.
const Redacted = "[REDACTED]"

// Secret wraps a value that must never be logged. It prints as [REDACTED]
// in logs, fmt and JSON; Reveal returns the real value.
type Secret string

// Reveal returns the wrapped value.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer.
func (Secret) String() string { return Redacted }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return Redacted }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalText implements encoding.TextMarshaler.
func (Secret) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

// sensitiveKeys are attribute names whose values are always replaced, on any
// group level, whatever their type.
var sensitiveKeys = map[string]bool{
	"password":      true,
	"passwd":        true,
	"pass":          true,
	"secret":        true,
	"token":         true,
	"pairing_token": true,
	"api_token":     true,
	"invite_token":  true,
	"session_id":    true,
	"cookie":        true,
	"set_cookie":    true,
	"authorization": true,
	"master_key":    true,
	"private_key":   true,
	"rcon_password": true,
	"db_url":        true,
	"dsn":           true,
}

// IsSensitiveKey reports whether an attribute name always holds a secret.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	if sensitiveKeys[k] {
		return true
	}
	return strings.HasSuffix(k, "_password") || strings.HasSuffix(k, "_secret") ||
		strings.HasSuffix(k, "_token")
}

// urlUserinfo matches credentials in URLs such as postgres://user:pw@host.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]+@`)

// RedactString removes credentials embedded in URLs from free text such as
// error messages.
func RedactString(s string) string {
	if !strings.Contains(s, "@") {
		return s
	}
	return urlUserinfo.ReplaceAllString(s, "${1}"+Redacted+"@")
}

func redactAttr(a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		if s := a.Value.String(); strings.Contains(s, "@") {
			return slog.String(a.Key, RedactString(s))
		}
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, RedactString(err.Error()))
		}
	}
	return a
}
