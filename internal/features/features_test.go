// SPDX-License-Identifier: AGPL-3.0-or-later

package features

import "testing"

func TestNewRefusesBadFlags(t *testing.T) {
	for _, bad := range [][]Flag{{{Name: ""}}, {{Name: "Upper"}}, {{Name: "a.b"}}, {{Name: "a"}, {Name: "a"}},
		{{Name: "a", Operations: []string{"X"}}, {Name: "b", Operations: []string{"X"}}}} {
		if _, err := New(bad...); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
