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
	found, err := check("testdata/bad")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, f := range found {
		keys = append(keys, f.key())
	}
	slices.Sort(keys)
	want := []string{"audit PostB", "authz GetC", "cmd internal/other"}
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
	// One finding without exception, one exception without finding.
	if err != nil || report([]finding{{"authz", "GetC", ""}, {"audit", "PostB", ""}}, allowed, devnull) != 2 {
		t.Errorf("report: %v", err)
	}
	if _ = os.WriteFile(p, []byte("authz\n"), 0o600); func() error { _, err := readExceptions(p); return err }() == nil {
		t.Error("a line without symbol is accepted")
	}
}
