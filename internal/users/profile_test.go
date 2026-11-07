// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

type profileFixture struct {
	*fixture
	p      *Profiles
	h      *password.Hasher
	others []string
}

func newProfiles(t *testing.T) *profileFixture {
	t.Helper()
	f := withMore(t)
	h, err := password.NewHasher(password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pf := &profileFixture{fixture: f, h: h}
	pf.p = NewProfiles(f.svc, h, func(_ context.Context, id, keep, _, _ string) error {
		pf.others = append(pf.others, id+"/"+keep)
		return nil
	})
	return pf
}

func TestProfileUpdate(t *testing.T) {
	f := newProfiles(t)
	ctx := context.Background()
	name, real := "Moddy", "Max Muster"
	contacts := map[string]string{"telegram": "@max"}
	u, err := f.p.Update(ctx, f.as("m"), ProfileUpdate{Pseudonym: &name, RealName: &real, Contacts: &contacts})
	if err != nil {
		t.Fatal(err)
	}
	if u.Pseudonym != "Moddy" || u.RealName != "Max Muster" || u.Contacts["telegram"] != "@max" {
		t.Errorf("profile = %+v", u)
	}
	var inv *InvalidError
	taken := "sa"
	bad := map[string]string{"Tele Gram": "x"}
	ctl := "a\x07b"
	for name, up := range map[string]ProfileUpdate{
		"taken pseudonym": {Pseudonym: &taken},
		"bad contact key": {Contacts: &bad},
		"control in name": {RealName: &ctl},
	} {
		if _, err := f.p.Update(ctx, f.as("m"), up); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The emergency admin keeps his pseudonym.
	emID := storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "admin2", Role: "rcon_admin", Emergency: true})
	f.ids["em"] = emID
	other := "boss"
	if _, err := f.p.Update(ctx, f.as("em"), ProfileUpdate{Pseudonym: &other}); !errors.As(err, &inv) {
		t.Errorf("emergency admin renamed: %v", err)
	}
	// The audit names fields, not content.
	if f.count(`SELECT count(*) FROM audit_log WHERE action = 'profile.update' AND payload LIKE '%Muster%'`) != 0 {
		t.Error("profile content in the audit")
	}
}

// TestChangePassword covers the second half of AUTH-007: after a change
// the old password no longer works, and other sessions end.
func TestChangePassword(t *testing.T) {
	f := newProfiles(t)
	ctx := context.Background()
	old, err := f.h.Hash(ctx, "initial password 1")
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE users SET password_hash = ? WHERE id = ?`, old, f.ids["m"])
	req := f.as("m")
	req.SessionID = "current-session"

	if err := f.p.ChangePassword(ctx, req, "wrong", "a brand new password"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong current password: %v", err)
	}
	var inv *InvalidError
	if err := f.p.ChangePassword(ctx, req, "initial password 1", "short"); !errors.As(err, &inv) {
		t.Errorf("short new password: %v", err)
	}
	if err := f.p.ChangePassword(ctx, req, "initial password 1", "initial password 1"); !errors.As(err, &inv) {
		t.Errorf("the same password again: %v", err)
	}
	if err := f.p.ChangePassword(ctx, req, "initial password 1", "a brand new password"); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, f.ids["m"]).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.h.Verify(ctx, hash, "initial password 1"); ok {
		t.Error("old password still valid")
	}
	if ok, _ := f.h.Verify(ctx, hash, "a brand new password"); !ok {
		t.Error("new password not set")
	}
	if len(f.others) != 1 || f.others[0] != f.ids["m"]+"/current-session" {
		t.Errorf("other sessions ended: %v", f.others)
	}
}

func encode(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withExif inserts an APP1 Exif segment after the JPEG start marker.
func withExif(jpg []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), []byte("GPS secret location")...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

func TestAvatar(t *testing.T) {
	f := newProfiles(t)
	ctx := context.Background()
	big := encode(t, "png", 1200, 600)
	if err := f.p.SetAvatar(ctx, f.as("m"), big); err != nil {
		t.Fatal(err)
	}
	data, typ, err := f.p.Avatar(ctx, f.as("m"), f.ids["m"])
	if err != nil || typ != "image/png" {
		t.Fatalf("own avatar: %q %v", typ, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 512 || cfg.Height != 256 {
		t.Errorf("stored size = %dx%d %v, want 512x256", cfg.Width, cfg.Height, err)
	}

	jpg := withExif(encode(t, "jpeg", 64, 64))
	if !bytes.Contains(jpg, []byte("GPS secret")) {
		t.Fatal("test image has no Exif")
	}
	if err := f.p.SetAvatar(ctx, f.as("m"), jpg); err != nil {
		t.Fatal(err)
	}
	data, typ, _ = f.p.Avatar(ctx, f.as("m"), f.ids["m"])
	if typ != "image/jpeg" || bytes.Contains(data, []byte("GPS secret")) || bytes.Contains(data, []byte("Exif")) {
		t.Errorf("metadata kept: type %s", typ)
	}

	var inv *InvalidError
	for name, d := range map[string][]byte{
		"gif":         encode(t, "gif", 10, 10),
		"html":        []byte("<script>alert(1)</script>"),
		"too large":   bytes.Repeat([]byte{0}, MaxAvatarBytes+1),
		"too many px": encode(t, "png", 4097, 1),
	} {
		if err := f.p.SetAvatar(ctx, f.as("m"), d); !errors.As(err, &inv) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}

	// W4: managers see it, others do not.
	if _, _, err := f.p.Avatar(ctx, f.as("sa"), f.ids["m"]); err != nil {
		t.Errorf("manager: %v", err)
	}
	if _, _, err := f.p.Avatar(ctx, f.as("m2"), f.ids["m"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("other moderator: %v", err)
	}
	if err := f.p.SetAvatar(ctx, f.as("m"), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.p.Avatar(ctx, f.as("m"), f.ids["m"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed avatar: %v", err)
	}
}

// TestResetPassword covers the reset part of AUTH-010: only for managed
// users, all sessions end, and the temporary password is in the answer
// only, never in log or audit.
func TestResetPassword(t *testing.T) {
	f := newProfiles(t)
	ctx := context.Background()
	pw, err := f.p.ResetPassword(ctx, f.as("sa"), f.ids["m"])
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	var must int
	if err := f.st.DB.QueryRowContext(ctx, `SELECT password_hash, must_change_password FROM users WHERE id = ?`, f.ids["m"]).Scan(&hash, &must); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.h.Verify(ctx, hash, pw); !ok || must != 1 {
		t.Errorf("reset not stored: verify=%v must=%d", ok, must)
	}
	if !slices.Contains(f.revoked, f.ids["m"]) {
		t.Error("sessions not ended")
	}
	if strings.Contains(f.logs.String(), pw) || f.count(`SELECT count(*) FROM audit_log WHERE payload LIKE ?`, "%"+pw+"%") != 0 {
		t.Error("temporary password in log or audit")
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE action = 'password.reset' AND result = 'ok' AND target_id = ?`, f.ids["m"]) != 1 {
		t.Error("reset not audited")
	}
	// Own password change clears the flag.
	req := f.as("m")
	if err := f.p.ChangePassword(ctx, req, pw, "my own new password"); err != nil {
		t.Fatal(err)
	}
	if f.count(`SELECT must_change_password FROM users WHERE id = ?`, f.ids["m"]) != 0 {
		t.Error("flag not cleared")
	}
	for name, target := range map[string]string{"unmanaged": f.ids["m2"], "server admin": f.ids["sa2"]} {
		if _, err := f.p.ResetPassword(ctx, f.as("sa"), target); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
