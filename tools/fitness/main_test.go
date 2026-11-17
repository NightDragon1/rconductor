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
	want := []string{"audit PostB", "authz GetC", "cmd internal/other", "redact internal/deep", "redact internal/other", "redact internal/pointer", "redact internal/printer", "redact internal/printer", "redact internal/printer"}
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
