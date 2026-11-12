// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// recoveryUser creates name with an address and the second factor on.
func (f *sfFixture) recoveryUser(t *testing.T, name string) {
	t.Helper()
	f.user(t, storetest.User{Pseudonym: name})
	p, _, _ := f.login(t, name, client)
	if err := f.svc.SetEmail(context.Background(), p, client, name+"@example.org", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	f.enable(t, name)
}

func (f *sfFixture) mailsTo(addr string) int {
	n := 0
	for _, m := range f.mail.all() {
		if m.to == addr {
			n++
		}
	}
	return n
}

// TestRecoveryLimitPerClientC8: the limit per client counts an IPv6 client
// per /64, so new addresses inside it share one count (D-076).
func TestRecoveryLimitPerClientC8(t *testing.T) {
	f := newSFFixture(t)
	f.recoveryUser(t, "kiwi")
	f.recoveryUser(t, "pear")
	f.mail.sent = nil
	ctx := context.Background()
	ip := netip.MustParseAddr("2001:db8:1:2::1")
	for range DefaultRecoveryPerHour {
		f.svc.RequestRecovery(ctx, "kiwi", Client{IP: ip, RequestID: "r"})
		ip = ip.Next()
	}
	f.svc.RequestRecovery(ctx, "pear", Client{IP: ip, RequestID: "r"})
	f.svc.RequestRecovery(ctx, "pear", Client{IP: netip.MustParseAddr("2001:db8:1:2:ffff::9"), RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("kiwi@example.org"); n != DefaultRecoveryPerHour {
		t.Errorf("mails to kiwi = %d, want %d", n, DefaultRecoveryPerHour)
	}
	if n := f.mailsTo("pear@example.org"); n != 0 {
		t.Errorf("mails to pear from the same /64 = %d, want 0", n)
	}
	// Another /64 has a count of its own.
	f.svc.RequestRecovery(ctx, "pear", Client{IP: netip.MustParseAddr("2001:db8:1:3::1"), RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("pear@example.org"); n != 1 {
		t.Errorf("mails to pear from another /64 = %d, want 1", n)
	}
}

// TestRecoveryFullTableKeepsCountsC8: a full client table is never cleared
// as a whole and does not shut out new clients: the count per user holds
// across any number of clients, known clients keep their own counts, and
// all new clients share one overflow window with the limit of one client
// until windows end (D-076, D-077).
func TestRecoveryFullTableKeepsCountsC8(t *testing.T) {
	f := newSFFixture(t)
	for _, name := range []string{"kiwi", "pear", "plum", "fig"} {
		f.recoveryUser(t, name)
	}
	f.mail.sent = nil
	ctx := context.Background()
	first := netip.MustParseAddr("10.0.0.1")
	ip := first
	for range maxRecoveryKeys + 10 {
		f.svc.RequestRecovery(ctx, "kiwi", Client{IP: ip, RequestID: "r"})
		ip = ip.Next()
	}
	f.svc.WaitMail()
	if n := f.mailsTo("kiwi@example.org"); n != DefaultRecoveryPerHour {
		t.Errorf("mails to kiwi within the hour = %d, want %d", n, DefaultRecoveryPerHour)
	}
	// The ten clients past the full table used up the overflow window:
	// further new clients are limited, also for another user.
	f.svc.RequestRecovery(ctx, "pear", Client{IP: ip, RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("pear@example.org"); n != 0 {
		t.Errorf("mails to pear over the overflow limit = %d, want 0", n)
	}
	// A known client keeps its own count.
	f.svc.RequestRecovery(ctx, "plum", Client{IP: first, RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("plum@example.org"); n != 1 {
		t.Errorf("mails to plum from a known client = %d, want 1", n)
	}
	// The ten clients past the full table and pear's client are reported
	// with their number (C39).
	total := 0
	for _, e := range fullEntries(t, f.logs.String()) {
		total += e.Count
	}
	if total != 11 {
		t.Errorf("new clients on the full table counted %d, want 11", total)
	}
	// After the hour the ended windows make room again.
	f.now = f.now.Add(time.Hour)
	f.svc.RequestRecovery(ctx, "pear", Client{IP: ip, RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("pear@example.org"); n != 1 {
		t.Errorf("mails to pear after the hour = %d, want 1", n)
	}
}

// TestRecoveryOverflowServesNewClientsC8: on a full client table new
// clients are served through the shared overflow window until its limit
// applies (D-077).
func TestRecoveryOverflowServesNewClientsC8(t *testing.T) {
	f := newSFFixture(t)
	for _, name := range []string{"kiwi", "pear", "plum", "fig"} {
		f.recoveryUser(t, name)
	}
	f.mail.sent = nil
	ctx := context.Background()
	ip := netip.MustParseAddr("10.0.0.1")
	for range maxRecoveryKeys {
		f.svc.RequestRecovery(ctx, "nobody", Client{IP: ip, RequestID: "r"})
		ip = ip.Next()
	}
	for _, name := range []string{"kiwi", "pear", "plum", "fig"} {
		ip = ip.Next()
		f.svc.RequestRecovery(ctx, name, Client{IP: ip, RequestID: "r"})
	}
	f.svc.WaitMail()
	served := 0
	for _, name := range []string{"kiwi", "pear", "plum", "fig"} {
		served += f.mailsTo(name + "@example.org")
	}
	if served != DefaultRecoveryPerHour {
		t.Errorf("new clients served on a full table = %d, want %d", served, DefaultRecoveryPerHour)
	}
	if f.mailsTo("fig@example.org") != 0 {
		t.Error("new client past the overflow limit was served")
	}
}

func countLines(logs, msg string) int {
	n := 0
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, msg) {
			n++
		}
	}
	return n
}
