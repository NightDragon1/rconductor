// SPDX-License-Identifier: AGPL-3.0-or-later

package totp

import (
	"strings"
	"testing"
	"time"
)

// TestRFC4226 covers the HOTP test vectors of RFC 4226, appendix D: the
// code of a counter is the code of a step (D-070).
func TestRFC4226(t *testing.T) {
	secret := []byte("12345678901234567890")
	for counter, want := range []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	} {
		if got := Code(secret, int64(counter)); got != want {
			t.Errorf("count=%d: %s, want %s", counter, got, want)
		}
	}
}

// TestRFC6238 covers the SHA-1 test vectors of RFC 6238, appendix B, cut
// to 6 digits.
func TestRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := Code(secret, Step(time.Unix(unix, 0))); got != want {
			t.Errorf("T=%d: %s, want %s", unix, got, want)
		}
	}
}

// TestVerify covers AUTH-008: a code of the current step and one step
// either way passes and names its step; older, newer and malformed codes
// fail.
func TestVerify(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1234567890, 0)
	cur := Step(now)
	for d := int64(-1); d <= 1; d++ {
		if step, ok := Verify(secret, Code(secret, cur+d), now); !ok || step != cur+d {
			t.Errorf("skew %d: %d %v", d, step, ok)
		}
	}
	for _, bad := range []string{Code(secret, cur-2), Code(secret, cur+2), "", "12345", "1234567", "12a456", strings.Repeat("9", 1000)} {
		if _, ok := Verify(secret, bad, now); ok {
			t.Errorf("%.10q passed", bad)
		}
	}
	// Spaces the user typed are fine.
	c := Code(secret, cur)
	if _, ok := Verify(secret, " "+c[:3]+" "+c[3:]+" ", now); !ok {
		t.Error("code with spaces refused")
	}
}

func TestURI(t *testing.T) {
	u := URI("RCONductor", "kiwi bird", []byte("12345678901234567890"))
	if !strings.HasPrefix(u, "otpauth://totp/RCONductor:kiwi%20bird?") || !strings.Contains(u, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") ||
		!strings.Contains(u, "digits=6") || !strings.Contains(u, "period=30") {
		t.Errorf("uri = %s", u)
	}
}
