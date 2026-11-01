// SPDX-License-Identifier: AGPL-3.0-or-later

package buildinfo

import "testing"

func TestMajor(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	cases := map[string]int{
		"1.2.3":      1,
		"v2.0.0":     2,
		"0.0.0-dev":  0,
		"10.1.0-rc1": 10,
		"dev":        -1,
		"":           -1,
	}
	for in, want := range cases {
		Version = in
		if got := Major(); got != want {
			t.Errorf("Major(%q) = %d, want %d", in, got, want)
		}
	}
}
