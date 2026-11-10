// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"runtime"
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
// queue full a request is dropped: the answer is the same, the drop is
// logged with its event, and no goroutine is left for it (D-077).
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
				f.svc.RequestRecovery(ctx, "kiwi", Client{IP: netip.AddrFrom4([4]byte{198, 51, 100, byte(n)}), RequestID: "r"})
			}()
			synctest.Wait()
			select {
			case <-done:
				return true
			default:
				return false
			}
		}
		// The first job holds the one worker, the second waits in the queue.
		for n := range 2 {
			if !ask(n + 1) {
				t.Fatalf("answer %d waits for the background", n+1)
			}
		}
		before := runtime.NumGoroutine()
		if !ask(3) {
			t.Error("answer with the queue full waits")
		}
		if after := runtime.NumGoroutine(); after != before {
			t.Errorf("goroutines %d after a dropped request, %d before", after, before)
		}
		if n := countLines(f.logs.String(), `"event":"auth.recovery.dropped"`); n != 1 {
			t.Errorf("drop logged %d times, want once", n)
		}
		unhold()
		f.svc.WaitMail()
		if n := f.mailsTo("kiwi@example.org"); n != 2 {
			t.Errorf("mails = %d, want 2 of the queued jobs", n)
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
