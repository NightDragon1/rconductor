// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// holdHandler holds every log record with message msg until release is
// closed.
type holdHandler struct {
	slog.Handler
	msg     string
	release chan struct{}
}

func (h holdHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.msg {
		<-h.release
	}
	return h.Handler.Handle(ctx, r)
}

func (h holdHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return holdHandler{h.Handler.WithAttrs(as), h.msg, h.release}
}

func (h holdHandler) WithGroup(name string) slog.Handler {
	return holdHandler{h.Handler.WithGroup(name), h.msg, h.release}
}

// TestRecoveryAnswerAloneC9: the answer to a recovery request waits only
// for the limit per client, for a known user as for an unknown name; the
// work for a known user (stored link, audit, log, mail) does not delay it
// (AUTH-008). The known user's work is held at its log line; the answer
// must come while it is held. The half for an unknown name also held
// before this property was fixed; it guards that both stay alike.
func TestRecoveryAnswerAloneC9(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSFFixture(t)
		f.recoveryUser(t, "kiwi")
		f.mail.sent = nil
		release := make(chan struct{})
		f.svc.log = slog.New(holdHandler{f.svc.log.Handler(), "recovery requested", release})
		ctx := context.Background()
		for _, name := range []string{"kiwi", "nobody"} {
			done := make(chan struct{})
			go func() {
				defer close(done)
				f.svc.RequestRecovery(ctx, name, client)
			}()
			synctest.Wait()
			select {
			case <-done:
			default:
				t.Errorf("answer for %q waits for the work of a known user", name)
			}
		}
		if n := len(f.mail.all()); n != 0 {
			t.Errorf("mail sent while the work is held: %d", n)
		}
		close(release)
		f.svc.WaitMail()
		if n := f.mailsTo("kiwi@example.org"); n != 1 {
			t.Errorf("mails to kiwi = %d, want 1", n)
		}
	})
}

// TestRecoveryPoolDropsWhenFullC9: the background part of recovery
// requests runs in a pool of fixed size with a bounded queue. With the
// queue full a request is dropped: the answer is the same and comes at
// once, the drop is logged with its event and counted exactly, and the
// dropped request does no recovery work: it is neither queued nor starts a
// worker, and no stored link, audit entry, log line or mail exists for it
// (D-077). The test checks the pool and the results of the work rather
// than the number of goroutines in the process, which also counts
// goroutines outside the test.
func TestRecoveryPoolDropsWhenFullC9(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSFFixture(t)
		f.recoveryUser(t, "kiwi")
		f.set(t, "auth.recovery_workers", "1")
		f.set(t, "auth.recovery_queue", "1")
		f.mail.sent = nil
		release := make(chan struct{})
		unhold := sync.OnceFunc(func() { close(release) })
		defer f.svc.WaitMail()
		defer unhold()
		f.svc.log = slog.New(holdHandler{f.svc.log.Handler(), "recovery requested", release})
		ctx := context.Background()
		ask := func(n int) bool {
			done := make(chan struct{})
			go func() {
				defer close(done)
				f.svc.RequestRecovery(ctx, "kiwi", Client{IP: netip.AddrFrom4([4]byte{198, 51, 100, byte(n)}), RequestID: fmt.Sprintf("r%d", n)})
			}()
			synctest.Wait()
			select {
			case <-done:
				return true
			default:
				return false
			}
		}
		pool := func() (queued, running int) {
			f.svc.sf.poolMu.Lock()
			defer f.svc.sf.poolMu.Unlock()
			return len(f.svc.sf.queue), f.svc.sf.running
		}
		// The first job holds the one worker, the second waits in the queue.
		for n := range 2 {
			if !ask(n + 1) {
				t.Fatalf("answer %d waits for the background", n+1)
			}
		}
		if q, r := pool(); q != 1 || r != 1 {
			t.Fatalf("pool before the drops: %d queued, %d running, want 1 and 1", q, r)
		}
		const drops = 3
		for n := 3; n < 3+drops; n++ {
			if !ask(n) {
				t.Errorf("answer %d with the queue full waits", n)
			}
		}
		if q, r := pool(); q != 1 || r != 1 {
			t.Errorf("pool after the drops: %d queued, %d running, want 1 and 1", q, r)
		}
		// The first drop is reported at once.
		if n := countLines(f.logs.String(), `"event":"auth.recovery.dropped"`); n != 1 {
			t.Errorf("drop entries before the window ends = %d, want 1", n)
		}
		unhold()
		f.svc.WaitMail()
		logs := f.logs.String()
		dropped := 0
		for line := range strings.SplitSeq(logs, "\n") {
			if !strings.Contains(line, `"event":"auth.recovery.dropped"`) {
				continue
			}
			var e struct {
				Dropped int `json:"dropped"`
			}
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatal(err)
			}
			dropped += e.Dropped
		}
		if dropped != drops {
			t.Errorf("drops counted %d, want %d", dropped, drops)
		}
		if n := f.mailsTo("kiwi@example.org"); n != 2 {
			t.Errorf("mails = %d, want 2 of the queued jobs", n)
		}
		// The stored link is the one of the last mail, so no dropped request
		// stored one after the queued jobs.
		if sent := f.mail.all(); len(sent) > 0 {
			_, token, _ := strings.Cut(sent[len(sent)-1].link, "#")
			if n := f.count(t, `SELECT count(*) FROM totp_recovery WHERE token_hash = ?`, HashToken(token)); n != 1 {
				t.Error("stored link is not the one of the last queued job")
			}
		}
		if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'totp.recovery_request'`); n != 2 {
			t.Errorf("audit entries = %d, want 2 of the queued jobs", n)
		}
		if n := countLines(logs, `"event":"auth.recovery.requested"`); n != 2 {
			t.Errorf("recovery log lines = %d, want 2 of the queued jobs", n)
		}
		for n := 3; n < 3+drops; n++ {
			id := fmt.Sprintf(`"r%d"`, n)
			if strings.Contains(logs, id) || f.count(t, `SELECT count(*) FROM audit_log WHERE payload LIKE ?`, "%"+id+"%") != 0 {
				t.Errorf("work done for the dropped request %s", id)
			}
		}
	})
}

// TestRecoveryDropReportPerWindowC9: dropped recovery jobs are reported at
// most once per window, each entry with the number dropped since the last
// one; together they count every drop, also those of the last window
// (D-077).
func TestRecoveryDropReportPerWindowC9(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSFFixture(t)
		f.recoveryUser(t, "kiwi")
		f.set(t, "auth.recovery_workers", "1")
		f.set(t, "auth.recovery_queue", "1")
		release := make(chan struct{})
		unhold := sync.OnceFunc(func() { close(release) })
		defer f.svc.WaitMail()
		defer unhold()
		f.svc.log = slog.New(holdHandler{f.svc.log.Handler(), "recovery requested", release})
		ctx := context.Background()
		ip := netip.MustParseAddr("10.0.0.1")
		ask := func() {
			f.svc.RequestRecovery(ctx, "kiwi", Client{IP: ip, RequestID: "r"})
			ip = ip.Next()
		}
		// One job holds the worker, one waits; all further ones are dropped.
		ask()
		synctest.Wait()
		ask()
		const drops = 1000
		for i := range drops {
			ask()
			if i%100 == 99 {
				time.Sleep(25 * time.Second)
			}
		}
		time.Sleep(2 * dropReportGap)
		synctest.Wait()
		unhold()
		f.svc.WaitMail()
		total := 0
		var last time.Time
		entries := 0
		for line := range strings.SplitSeq(f.logs.String(), "\n") {
			if !strings.Contains(line, `"event":"auth.recovery.dropped"`) {
				continue
			}
			var e struct {
				TS      time.Time `json:"ts"`
				Dropped int       `json:"dropped"`
			}
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatal(err)
			}
			if entries > 0 && e.TS.Sub(last) < dropReportGap {
				t.Errorf("entries %v apart, window %v", e.TS.Sub(last), dropReportGap)
			}
			last = e.TS
			entries++
			total += e.Dropped
		}
		if total != drops {
			t.Errorf("dropped counted %d in %d entries, want %d", total, entries, drops)
		}
		// 250 s of drops make at most one entry per begun window.
		if entries < 2 || entries > 6 {
			t.Errorf("entries = %d, want one per window", entries)
		}
	})
}
