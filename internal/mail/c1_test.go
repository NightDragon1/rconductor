// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store/secret"
)

// TestC1PasswordState (NFR-017): the view tells whether the password is
// missing, stored and readable, or stored but sealed with another master
// key; the password itself never leaves.
func TestC1PasswordState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	state := func() View {
		t.Helper()
		v, err := f.svc.Get(ctx, f.admin)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := state(); v.PasswordState != PasswordNone || v.PasswordSet {
		t.Errorf("without a password: %+v", v)
	}
	seal := func(box *secret.Box) {
		t.Helper()
		sealed, err := box.Seal("s3cret-pw", secretContext())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.DB.ExecContext(ctx, `UPDATE smtp_settings SET password = ? WHERE id = 1`, sealed); err != nil {
			t.Fatal(err)
		}
	}
	seal(f.svc.box)
	if v := state(); v.PasswordState != PasswordSet || !v.PasswordSet {
		t.Errorf("readable password: %+v", v)
	}
	other := make([]byte, 32)
	_, _ = rand.Read(other)
	otherBox, err := secret.New(other)
	if err != nil {
		t.Fatal(err)
	}
	seal(otherBox)
	if v := state(); v.PasswordState != PasswordUnreadable || !v.PasswordSet {
		t.Errorf("password of another master key: %+v", v)
	}
	// A damaged value cannot be read either.
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE smtp_settings SET password = 'v1:broken' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if v := state(); v.PasswordState != PasswordUnreadable {
		t.Errorf("damaged password: %+v", v)
	}
	// Entering it again makes it readable; the answer of the write says so.
	cur := state()
	in := Input{Host: ptr("smtp.example.org"), Port: ptr(587), Encryption: ptr(EncryptionSTARTTLS),
		Username: ptr("mailer"), Password: ptr(logging.Secret("new-pw-123")), From: ptr("panel@example.org")}
	v, err := f.svc.Update(ctx, f.admin, in, &cur.Version)
	if err != nil || v.PasswordState != PasswordSet {
		t.Fatalf("after entering it again: %+v, %v", v, err)
	}
}
