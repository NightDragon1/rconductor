// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestEachRuleFindsItsViolation runs the checks on a module that breaks
// every rule once.
func TestEachRuleFindsItsViolation(t *testing.T) {
	found, err := check("testdata/bad", map[string]string{"internal/printer": "test: a destination without a conversion", "internal/dest": "test: a destination"})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, f := range found {
		keys = append(keys, f.key())
	}
	slices.Sort(keys)
	// internal/quiet (a buffer, slog.DiscardHandler), internal/e2e (the
	// harness), internal/logging (its own logger) and internal/rcon (a
	// revealer with a method value) break no rule.
	want := []string{"audit PostB", "authz GetC", "cmd internal/other", "redact internal/boundreveal", "redact internal/bprint",
		"redact internal/deep", "redact internal/fprint", "redact internal/oswrite", "redact internal/other", "redact internal/ownlog",
		"redact internal/pointer", "redact internal/printer", "redact internal/printer", "redact internal/printer", "redact internal/rawiface", "redact internal/rawline", "redact internal/rawlogs", "redact internal/rawparam", "redact internal/rawvalue", "redact internal/rawvar",
		"redact internal/reflectany", "redact internal/reflectsecret", "redact internal/reflectstruct", "redact internal/revealiface", "redact internal/servers", "redact internal/stdlog", "redact internal/sysbypass", "redact internal/sysiface", "redact internal/sysvalue", "redact internal/thunk"}
	if !slices.Equal(keys, want) {
		t.Errorf("findings = %v, want %v", keys, want)
	}
}

func TestExceptions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "exc")
	_ = os.WriteFile(p, []byte("# c\nauthz GetC # reason\ncmd gone # no longer found\n"), 0o600)
	allowed, err := readExceptions(p)
	devnull, _ := os.Open(os.DevNull)
	defer func() { _ = devnull.Close() }()
	if err != nil || report([]finding{{"authz", "GetC", ""}, {"audit", "PostB", ""}}, allowed, devnull) != 2 {
		t.Errorf("report: %v", err)
	}
	if _ = os.WriteFile(p, []byte("authz\n"), 0o600); func() error { _, err := readExceptions(p); return err }() == nil {
		t.Error("a line without symbol is accepted")
	}
	// C252: a blank other than space or tab, which the awk of the ratchet
	// does not split at, is refused.
	if _ = os.WriteFile(p, []byte("authz\u00a0GetC # reason\n"), 0o600); func() error { _, err := readExceptions(p); return err }() == nil {
		t.Error("a no-break space between the fields is accepted")
	}
	if _ = os.WriteFile(p, []byte("authz\tGetC # reason\n"), 0o600); func() error { _, err := readExceptions(p); return err }() != nil {
		t.Error("a tab between the fields is refused")
	}
}

// TestDestinations: every line names a package and a reason; a missing
// file is an error, so the rule never runs without its list.
func TestDestinations(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dest")
	for body, ok := range map[string]bool{
		"# c\ninternal/auth # reason\n\n": true,
		"internal/auth\n":                 false,
		"internal/auth #\n":               false,
		"internal/a internal/b # r\n":     false,
		"\u00a0 # r\n":                    false, // C252: no blank but space or tab
		"\u00a0internal/x # r\n":          false,
		"\tinternal/auth\t# r\n":          true,
	} {
		_ = os.WriteFile(p, []byte(body), 0o600)
		if _, err := readDestinations(p); (err == nil) != ok {
			t.Errorf("%q: %v", body, err)
		}
	}
	if _, err := readDestinations(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing file passes")
	}
}
