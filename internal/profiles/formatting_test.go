// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"strings"
	"testing"
)

// TestFormattingCheck covers CMD-017 and CAT-006: formatting takes only a
// known value; without it the codes are removed (SEC-006).
func TestFormattingCheck(t *testing.T) {
	for _, v := range []string{"", "formatting: minecraft\n"} {
		p, err := Parse([]byte(markerBase + v))
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if want := strings.TrimSpace(strings.TrimPrefix(v, "formatting:")); p.Formatting != want {
			t.Errorf("%q: formatting = %q", v, p.Formatting)
		}
	}
	for _, v := range []string{"html", "Minecraft", "\"minecraft \"", "\"\\u00a7\""} {
		_, err := Parse([]byte(markerBase + "formatting: " + v + "\n"))
		if err == nil || !strings.Contains(err.Error(), "formatting must be one of minecraft") {
			t.Errorf("%q: err = %v", v, err)
		}
	}
}
