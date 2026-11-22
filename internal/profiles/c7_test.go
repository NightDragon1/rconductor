// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import "testing"

// TestC7MaxLengthZero: syntax.max_length 0 written in the file is refused;
// a missing key takes the default (CAT-006, D-074).
func TestC7MaxLengthZero(t *testing.T) {
	if _, err := Parse([]byte(checkHead + "  - path: list\n    description: Players\nsyntax:\n  max_length: 0\n")); err == nil {
		t.Error("max_length 0 accepted")
	}
	p, err := Parse([]byte(checkHead + "  - path: list\n    description: Players\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Syntax.MaxLength != DefaultMaxLength {
		t.Errorf("max_length = %d, want %d", p.Syntax.MaxLength, DefaultMaxLength)
	}
	if _, err := Parse([]byte(checkHead + "  - path: list\n    description: Players\nsyntax:\n  max_length: 1\n")); err != nil {
		t.Errorf("max_length 1 = %v", err)
	}
}
