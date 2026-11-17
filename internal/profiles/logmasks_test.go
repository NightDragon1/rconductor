// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func withMasks(masks ...string) []byte {
	b, _ := json.Marshal(masks)
	return []byte(markerBase + "log_masks: " + string(b) + "\n")
}

// TestLogMasks covers D-088 b): the masking patterns of a profile are read,
// kept in the stored rules and checked at the import.
func TestLogMasks(t *testing.T) {
	p, err := Parse(withMasks(`(-rcon\s+)\S+`, `(?i)(adminpass=)\S+`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	var back Profile
	if err := json.Unmarshal(b, &back); err != nil || !slices.Equal(back.LogMasks, p.LogMasks) || len(back.LogMasks) != 2 {
		t.Errorf("stored rules lose the patterns: %s", b)
	}

	wide := "(" + strings.Repeat("[a-z]", 40) + ")"
	for name, masks := range map[string][]string{
		"not RE2":          {`(?<=a)b`},
		"not compiling":    {`(a`},
		"empty":            {``},
		"too long":         {strings.Repeat("a", MaxLogMaskLen+1)},
		"control char":     {"a\tb"},
		"program too big":  {`[a-z]{200}`},
		"too many":         slices.Repeat([]string{`a=\S+`}, MaxLogMasks+1),
		"profile too big":  {wide, wide, wide, wide, wide, wide, wide},
		"repeat over 1000": {`a{1001}`},
	} {
		_, err := Parse(withMasks(masks...))
		if err == nil || !strings.Contains(err.Error(), "log_masks") {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if n, _ := LogMaskInsts(wide); n > MaxLogMaskInsts || 7*n <= MaxLogMaskProfileInst {
		t.Fatalf("test pattern has %d instructions", n)
	}
	// At the bounds the profile is valid.
	if _, err := Parse(withMasks(slices.Repeat([]string{`(k=)\S+`}, MaxLogMasks)...)); err != nil {
		t.Errorf("at the bounds: %v", err)
	}
}

// TestLogMasksHoldFromEveryProfile: like wrappers and separators, the
// patterns of all profiles of a server hold together (D-088 b).
func TestLogMasksHoldFromEveryProfile(t *testing.T) {
	top := &Profile{ID: "top", LogMasks: []string{`(a=)\S+`, `(b=)\S+`}}
	low := &Profile{ID: "low", LogMasks: []string{`(b=)\S+`, `(c=)\S+`}}
	got := Effective([]*Profile{top, nil, low}).LogMasks
	if want := []string{`(a=)\S+`, `(b=)\S+`, `(c=)\S+`}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
