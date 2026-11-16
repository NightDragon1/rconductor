// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

func request(t *testing.T, s *Service, role perm.Role) users.Request {
	t.Helper()
	stored := role
	if !slicesContain([]string{"rcon_admin", "server_admin", "moderator", "supporter"}, string(role)) {
		stored = perm.Supporter // a session whose role is not known, on a stored user
	}
	id := storetest.CreateUser(t, s.st, storetest.User{Pseudonym: "u" + string(role), Role: string(stored)})
	return users.Request{Actor: perm.Actor{ID: id, Role: role}, Pseudonym: string(role)}
}

func denials(t *testing.T, s *Service, action string) int {
	t.Helper()
	var n int
	err := s.st.DB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = ? AND result = 'denied' AND reason_code = 'forbidden'`, action).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestSendRoles: only the RCON admin and the server admin send, each to
// his own audiences (D-085); every refusal is 403 and in the audit.
func TestSendRoles(t *testing.T) {
	s := NewService(storetest.New(t), nil)
	ctx := context.Background()
	reqs := map[perm.Role]users.Request{}
	for _, r := range []perm.Role{perm.RCONAdmin, perm.ServerAdmin, perm.Moderator, perm.Supporter, "unknown"} {
		reqs[r] = request(t, s, r)
	}
	audiences := []string{AudienceAll, AudienceServerAdmins, AudienceModerators, AudienceUsers, AudienceScope, AudienceScopeModerators, "nobody"}
	refused := 0
	for role, req := range reqs {
		for _, a := range audiences {
			in := Input{Text: "hi", Audience: a}
			if a == AudienceUsers {
				in.UserIDs = []string{"00000000-0000-4000-8000-000000000001"}
			}
			want := users.ErrForbidden
			if slicesContain(senders[role], a) {
				want = ErrNotImplemented
			} else {
				refused++
			}
			if err := s.Send(ctx, req, in); !errors.Is(err, want) {
				t.Errorf("%s to %s = %v, want %v", role, a, err, want)
			}
		}
	}
	if refused != 5*len(audiences)-6 {
		t.Fatalf("refused %d, the table changed", refused)
	}
	if n := denials(t, s, "message.send"); n != refused {
		t.Errorf("audited refusals = %d, want %d", n, refused)
	}
}

func slicesContain(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// TestSendInput: after the role, the input is checked (400): text, its
// bytes, user_ids only and always with audience users, their number and
// repeats. A refused role learns nothing about its input.
func TestSendInput(t *testing.T) {
	s := NewService(storetest.New(t), nil)
	ctx := context.Background()
	admin, sa, mod := request(t, s, perm.RCONAdmin), request(t, s, perm.ServerAdmin), request(t, s, perm.Moderator)
	id := func(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i) }
	many := make([]string, MaxUserIDs+1)
	for i := range many {
		many[i] = id(i)
	}
	invalid := []struct {
		name string
		req  users.Request
		in   Input
	}{
		{"empty text", admin, Input{Audience: AudienceAll}},
		{"text over 8 KiB", admin, Input{Text: strings.Repeat("ä", MaxTextBytes/2+1), Audience: AudienceAll}},
		{"users without user_ids", admin, Input{Text: "hi", Audience: AudienceUsers}},
		{"users with empty user_ids", admin, Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{}}},
		{"user_ids with all", admin, Input{Text: "hi", Audience: AudienceAll, UserIDs: []string{id(1)}}},
		{"user_ids with scope", sa, Input{Text: "hi", Audience: AudienceScope, UserIDs: []string{}}},
		{"too many user_ids", admin, Input{Text: "hi", Audience: AudienceUsers, UserIDs: many}},
		{"repeated user_ids", admin, Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{id(1), id(2), id(1)}}},
	}
	for _, c := range invalid {
		var inv *users.InvalidError
		if err := s.Send(ctx, c.req, c.in); !errors.As(err, &inv) {
			t.Errorf("%s = %v, want invalid", c.name, err)
		}
	}
	if err := s.Send(ctx, admin, Input{Text: strings.Repeat("a", MaxTextBytes), Audience: AudienceUsers, UserIDs: many[:MaxUserIDs]}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("at the bounds = %v, want not implemented", err)
	}
	if err := s.Send(ctx, mod, Input{Audience: AudienceUsers, UserIDs: many}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("moderator with invalid input = %v, want forbidden first", err)
	}
	if err := s.Send(ctx, sa, Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{id(1)}}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin to users = %v, want forbidden", err)
	}
}

// TestReadRoles: every known role may be a recipient, so the operations
// on one message come through to the message; a role that is not known is
// refused and audited.
func TestReadRoles(t *testing.T) {
	s := NewService(storetest.New(t), nil)
	ctx := context.Background()
	const id = "00000000-0000-4000-8000-000000000001"
	ops := map[string]func(users.Request) error{
		"message.list":      func(r users.Request) error { return s.List(ctx, r) },
		"message.read":      func(r users.Request) error { return s.Get(ctx, r, id) },
		"message.delete":    func(r users.Request) error { return s.Delete(ctx, r, id) },
		"message.mark_read": func(r users.Request) error { return s.MarkRead(ctx, r, id) },
	}
	for _, role := range []perm.Role{perm.RCONAdmin, perm.ServerAdmin, perm.Moderator, perm.Supporter} {
		req := request(t, s, role)
		for action, op := range ops {
			if err := op(req); !errors.Is(err, ErrNotImplemented) {
				t.Errorf("%s %s = %v", role, action, err)
			}
		}
	}
	for _, role := range []perm.Role{"", "unknown"} {
		req := request(t, s, role)
		for action, op := range ops {
			if err := op(req); !errors.Is(err, users.ErrForbidden) {
				t.Errorf("%q %s = %v, want forbidden", role, action, err)
			}
		}
	}
	for action := range ops {
		if n := denials(t, s, action); n != 2 {
			t.Errorf("audited refusals of %s = %d, want 2", action, n)
		}
	}
}

// TestRefusalsInParallel: parallel refusals each reach the audit.
func TestRefusalsInParallel(t *testing.T) {
	s := NewService(storetest.New(t), nil)
	mod := request(t, s, perm.Moderator)
	const n = 32
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			if err := s.Send(context.Background(), mod, Input{Text: "hi", Audience: AudienceAll}); !errors.Is(err, users.ErrForbidden) {
				t.Errorf("send = %v", err)
			}
		})
	}
	wg.Wait()
	if got := denials(t, s, "message.send"); got != n {
		t.Errorf("audited refusals = %d, want %d", got, n)
	}
}
