// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

import (
	"net/http"
	"testing"
	"time"
)

// D-104: auth.totp_required can be switched on only by a caller
// who has a second factor of his own. Without one: 409 with the reason
// own_totp_required, and nothing is written. With one: the switch works.
// The test leaves the switch off; the admin keeps his second factor, which
// does not matter for the one admin session of the other tests.
func TestD104TOTPRequiredNeedsOwnSecondFactor(t *testing.T) {
	a := admin(t)
	t.Cleanup(func() { totpRequiredOff(t, a) })

	// Abuse case: admin without second factor.
	before, etag := settings(t, a)
	if v, ok := before["auth.totp_required"]; ok && v != false {
		t.Fatalf("auth.totp_required at start = %v, want false", v)
	}
	r := a.do(http.MethodPatch, "/settings", map[string]any{"auth.totp_required": true}, "If-Match", etag)
	r.want(t, "switch on without own second factor", http.StatusConflict)
	if got := r.reason(t); got != "own_totp_required" {
		t.Fatalf("reason = %q, want own_totp_required", got)
	}
	after, etag2 := settings(t, a)
	if v := after["auth.totp_required"]; v == true {
		t.Fatal("refused change was written: auth.totp_required is on")
	}
	if etag2 != etag {
		t.Fatalf("refused change raised the version: ETag %q → %q", etag, etag2)
	}

	// Normal case: with an own second factor the switch works.
	ownSecondFactor(t, a)

	_, etag = settings(t, a)
	a.do(http.MethodPatch, "/settings", map[string]any{"auth.totp_required": true}, "If-Match", etag).
		want(t, "switch on with own second factor", http.StatusOK)
	on, etag := settings(t, a)
	if on["auth.totp_required"] != true {
		t.Fatalf("auth.totp_required = %v after the change, want true", on["auth.totp_required"])
	}

	_ = etag
}

var adminSecondFactor bool

// ownSecondFactor switches on the admin's own second factor once.
func ownSecondFactor(t *testing.T, a *client) {
	t.Helper()
	if adminSecondFactor {
		return
	}
	r := a.do(http.MethodPost, "/me/second-factor/setup", map[string]string{"password": adminNewPassword}).
		want(t, "second factor setup", http.StatusOK)
	var setup struct {
		Secret string `json:"secret"`
	}
	r.decode(t, &setup)
	a.do(http.MethodPost, "/me/second-factor/confirm", map[string]string{"code": totp(t, setup.Secret, time.Now())}).
		want(t, "second factor confirm", http.StatusNoContent)
	adminSecondFactor = true
}

// totpRequiredOff leaves auth.totp_required off for the other tests, also
// when the test failed on a build that let the switch on without a second
// factor (then the session only allows the setup, so that comes first).
func totpRequiredOff(t *testing.T, a *client) {
	r := a.do(http.MethodGet, "/settings", nil)
	if r.status == http.StatusForbidden {
		ownSecondFactor(t, a)
		r = a.do(http.MethodGet, "/settings", nil)
	}
	r.want(t, "GET /settings in cleanup", http.StatusOK)
	var m map[string]any
	r.decode(t, &m)
	if m["auth.totp_required"] == true {
		a.do(http.MethodPatch, "/settings", map[string]any{"auth.totp_required": false}, "If-Match", r.header.Get("ETag")).
			want(t, "switch off in cleanup", http.StatusOK)
	}
}
