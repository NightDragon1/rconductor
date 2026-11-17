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

	// Patterns of n instructions with a group 1, found by search.
	sized := func(n int) string {
		for k := 1; k < 200; k++ {
			p := "(k=)" + strings.Repeat(`\w`, k)
			if m, _ := LogMaskInsts(p); m == n {
				return p
			}
		}
		t.Fatalf("no pattern of %d instructions", n)
		return ""
	}
	atInsts, overInsts := sized(MaxLogMaskInsts), sized(MaxLogMaskInsts+1)
	half := sized(MaxLogMaskProfileInst / 2)
	long := "(k=)[" + strings.Repeat("a", MaxLogMaskLen-6) + "]"
	for name, c := range map[string]struct {
		masks []string
		want  string
	}{
		"not RE2":             {[]string{`(?<=a)b`}, "RE2"},
		"not compiling":       {[]string{`(a`}, "RE2"},
		"repeat over 1000":    {[]string{`(k=)a{1001}`}, "RE2"},
		"empty":               {[]string{``}, "printable"},
		"too long":            {[]string{long + "?"}, "printable"},
		"control char":        {[]string{"(k=)a\tb"}, "printable"},
		"bidi mark":           {[]string{"(k=)a\u202eb"}, "printable"},
		"no group":            {[]string{`k=\S+`}, "start with group 1"},
		"optional group":      {[]string{`(k=)?\S+`}, "start with group 1"},
		"group in a choice":   {[]string{`(k=)\S+|x`}, "start with group 1"},
		"group in repetition": {[]string{`(?:(k=)\S+)*`}, "start with group 1"},
		"group 1 never":       {[]string{`(?:(k=)\S+){0}x`}, "start with group 1"},
		"group 1 repeated":    {[]string{`(?:(k=)\d+){1,3}`}, "start with group 1"},
		"group 1 plus":        {[]string{`(k=)+\S+`}, "start with group 1"},
		"group 1 nested":      {[]string{`(?:a(k=))+\S+`}, "start with group 1"},
		"group 1 not first":   {[]string{`[0-9a-f]{32}( is your token)`}, "start with group 1"},
		"group 1 after text":  {[]string{`\S+(@host)`}, "start with group 1"},
		"group 2 optional":    {[]string{`(k=)\S+(@)?`}, "group 2 last"},
		"group in group 1":    {[]string{`(pass(word)?=)\S+`}, "no group in group 1"},
		"group 2 right after": {[]string{`(k=)(\S+)`}, "not right after"},
		"group 2 not last":    {[]string{`(k=)\S+(@)x`}, "group 2 last"},
		"group 3":             {[]string{`(k=)\S+(@)(x)`}, "at most groups"},
		"program too big":     {[]string{overInsts}, "instructions, at most"},
		"too many":            {slices.Repeat([]string{`(k=)\S+`}, MaxLogMasks+1), "at most 8"},
		"profile too big":     {[]string{half, half, `(j=)\S+`}, "in all"},
	} {
		_, err := Parse(withMasks(c.masks...))
		if err == nil || !strings.Contains(err.Error(), "log_masks") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// At the bounds the profile is valid.
	for name, masks := range map[string][]string{
		"count":        slices.Repeat([]string{`(k=)\S+`}, MaxLogMasks),
		"length":       {long},
		"instructions": {atInsts},
		"in all":       {half, half},
		"group 2":      {`(k=)\S+(@)`},
		"anchored":     {`^(k=)\S+`},
		"word start":   {`\b(k=)\S+`},
		"no capture":   {`(k=)(?:word)?\S+`},
	} {
		if _, err := Parse(withMasks(masks...)); err != nil {
			t.Errorf("at the bound %s: %v", name, err)
		}
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

// TestCompileLogMasks: stored patterns are checked again before they are
// compiled; a pattern the import would refuse is left out by position.
func TestCompileLogMasks(t *testing.T) {
	res, bad := CompileLogMasks([]string{`(k=)\S+`, `k=\S+`, "(k=)a\u202eb", `(a`, `(?i)(pw\s*=\s*)\S+`})
	if len(res) != 2 || !slices.Equal(bad, []int{1, 2, 3}) {
		t.Errorf("got %d patterns, bad %v", len(res), bad)
	}
	if res, bad := CompileLogMasks(nil); res != nil || bad != nil {
		t.Errorf("nil: %v %v", res, bad)
	}
}
