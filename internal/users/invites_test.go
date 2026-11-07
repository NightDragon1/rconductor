// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/perm"
)

const goodPassword = "a long enough password"

func newInvites(t *testing.T, f *fixture) *Invites {
	t.Helper()
	if err := EnsureDefaults(context.Background(), f.st); err != nil {
		t.Fatal(err)
	}
	h, err := password.NewHasher(password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return NewInvites(f.svc, h)
}

func TestInviteAndRedeem(t *testing.T) {
	f := withMore(t)
	iv := newInvites(t, f)
	ctx := context.Background()

	// sa invites a moderator with m as template.
	inv, token, err := iv.Create(ctx, f.as("sa"), perm.Moderator, f.ids["m"])
	if err != nil {
		t.Fatal(err)
	}
	if !inv.ExpiresAt.Equal(inv.CreatedAt.Add(DefaultInviteLifetime)) || len(token) < 40 {
		t.Errorf("invite = %+v, token length %d", inv, len(token))
	}
	if f.count(`SELECT count(*) FROM invites WHERE token_hash = ?`, token) != 0 {
		t.Error("token stored in plain")
	}

	u, err := iv.Redeem(ctx, token, " Nova ", goodPassword, "req-r")
	if err != nil {
		t.Fatal(err)
	}
	if u.Pseudonym != "Nova" || u.Role != perm.Moderator {
		t.Errorf("new user = %+v", u)
	}
	logtest.Wait(t, f.logs, `"event":"auth.invite.used"`)
	// The template's groups of sa were copied, sa2's group was not.
	a, err := f.svc.Assignments(ctx, f.as("sa"), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Groups, []string{"mg"}) {
		t.Errorf("copied groups = %v, want sa's group only", a.Groups)
	}
	r, _ := f.checker.For(ctx, u.ID)
	if _, ok := r.Server("s1"); !ok {
		t.Error("new moderator lacks the template's server")
	}

	// Single use.
	if _, err := iv.Redeem(ctx, token, "Other", goodPassword, "req"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("second use: %v", err)
	}
}

func TestRedeemRules(t *testing.T) {
	f := withMore(t)
	iv := newInvites(t, f)
	ctx := context.Background()
	_, token, err := iv.Create(ctx, f.as("admin"), perm.ServerAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	var inv *InvalidError
	for name, tc := range map[string][2]string{
		"reserved pseudonym": {"Admin", goodPassword},
		"too short":          {"x", goodPassword},
		"markup":             {"<b>x</b>", goodPassword},
		"taken":              {"m", goodPassword},
		"short password":     {"Fresh", "short"},
	} {
		if _, err := iv.Redeem(ctx, token, tc[0], tc[1], "req"); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := iv.Redeem(ctx, "made-up", "Fresh", goodPassword, "req"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("unknown token: %v", err)
	}
	iv.now = func() time.Time { return time.Now().Add(DefaultInviteLifetime + time.Minute) }
	if _, err := iv.Redeem(ctx, token, "Fresh", goodPassword, "req"); !errors.Is(err, ErrInviteInvalid) {
		t.Errorf("expired token: %v", err)
	}
}

func TestWhoMayInvite(t *testing.T) {
	f := withMore(t)
	iv := newInvites(t, f)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		actor string
		role  perm.Role
	}{
		"server admin invites a server admin": {"sa", perm.ServerAdmin},
		"RCON admin invites an RCON admin":    {"admin", perm.RCONAdmin},
		"supporter invites":                   {"sup", perm.Supporter},
		"moderator without release":           {"m", perm.Supporter},
	} {
		if _, _, err := iv.Create(ctx, f.as(tc.actor), tc.role, ""); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f.exec(`UPDATE settings SET value = 'true' WHERE key = ?`, perm.SettingSupportersEnabled)
	f.exec(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, f.ids["m"])
	if _, _, err := iv.Create(ctx, f.as("m"), perm.Supporter, ""); err != nil {
		t.Errorf("moderator with release: %v", err)
	}
	// A template must be managed and have the invited role.
	if _, _, err := iv.Create(ctx, f.as("sa"), perm.Moderator, f.ids["m2"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign template: %v", err)
	}
	if _, _, err := iv.Create(ctx, f.as("sa"), perm.Moderator, f.ids["sup"]); err == nil {
		t.Error("template with another role accepted")
	}
	list, _ := iv.List(ctx, f.as("m"))
	if len(list) != 1 {
		t.Errorf("moderator sees %d invites", len(list))
	}
	if err := iv.Delete(ctx, f.as("sa"), list[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete another's invite: %v", err)
	}
	if err := iv.Delete(ctx, f.as("m"), list[0].ID); err != nil {
		t.Errorf("delete own invite: %v", err)
	}
}
