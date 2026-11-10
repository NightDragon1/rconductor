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
// as a whole: the count per user holds across any number of clients, a new
// client is refused while the table is full, and room comes back only as
// windows end (D-076).
func TestRecoveryFullTableKeepsCountsC8(t *testing.T) {
	f := newSFFixture(t)
	f.recoveryUser(t, "kiwi")
	f.recoveryUser(t, "pear")
	f.mail.sent = nil
	ctx := context.Background()
	ip := netip.MustParseAddr("10.0.0.1")
	for range maxRecoveryKeys + 10 {
		f.svc.RequestRecovery(ctx, "kiwi", Client{IP: ip, RequestID: "r"})
		ip = ip.Next()
	}
	f.svc.WaitMail()
	if n := f.mailsTo("kiwi@example.org"); n != DefaultRecoveryPerHour {
		t.Errorf("mails to kiwi within the hour = %d, want %d", n, DefaultRecoveryPerHour)
	}
	// The table is full: a new client gets nothing, also for another user.
	f.svc.RequestRecovery(ctx, "pear", Client{IP: ip, RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("pear@example.org"); n != 0 {
		t.Errorf("mails to pear from a new client on a full table = %d, want 0", n)
	}
	if n := countLines(f.logs.String(), "recovery limit table full"); n != 1 {
		t.Errorf("full table reported %d times, want once", n)
	}
	// After the hour the ended windows make room again.
	f.now = f.now.Add(time.Hour)
	f.svc.RequestRecovery(ctx, "pear", Client{IP: ip, RequestID: "r"})
	f.svc.WaitMail()
	if n := f.mailsTo("pear@example.org"); n != 1 {
		t.Errorf("mails to pear after the hour = %d, want 1", n)
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
