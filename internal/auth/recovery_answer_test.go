// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
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
