// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"context"
	"crypto/rand"
	"testing"

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
}
