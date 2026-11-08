// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
)

// failure is a failed delivery with its category; the server's answer is
// not kept, it could repeat what was sent.
type failure struct{ category string }

func (f *failure) Error() string { return "mail not sent: " + f.category }

func fail(category string) error { return &failure{category: category} }

type dialSpec struct {
	host, encryption string
	port             int
	user             string
	password         logging.Secret
	roots            *x509.CertPool
	deadline         time.Time
}

// tlsConfig checks the certificate of the SMTP server, always: there is no
// switch to skip it (NFR-017).
func (d dialSpec) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: d.host, MinVersion: tls.VersionTLS12, RootCAs: d.roots}
}

// connectCategory tells a time-out from a refused or unknown host.
func connectCategory(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return CategoryTimeout
	}
	return CategoryConnect
}

func tlsCategory(err error) string {
	if c := connectCategory(err); c == CategoryTimeout {
		return c
	}
	return CategoryTLS
}

// deliver sends one message. SSL speaks TLS from the start; STARTTLS must
// be offered and succeed, else the delivery fails as tls and nothing goes
// out in plain text; none sends in plain text and never authenticates.
func deliver(ctx context.Context, d dialSpec, from, to string, msg []byte) error {
	addr := net.JoinHostPort(d.host, strconv.Itoa(d.port))
	dialer := &net.Dialer{Deadline: d.deadline}
	var conn net.Conn
	var err error
	if d.encryption == EncryptionSSL {
		td := &tls.Dialer{NetDialer: dialer, Config: d.tlsConfig()}
		conn, err = td.DialContext(ctx, "tcp", addr)
		if err != nil {
			var oe *net.OpError
			if errors.As(err, &oe) && oe.Op == "dial" {
				return fail(connectCategory(err))
			}
			return fail(tlsCategory(err))
		}
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fail(connectCategory(err))
		}
	}
	_ = conn.SetDeadline(d.deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	c, err := smtp.NewClient(conn, d.host)
	if err != nil {
		_ = conn.Close()
		return fail(connectCategory(err))
	}
	defer func() { _ = c.Close() }()
	if d.encryption == EncryptionSTARTTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fail(CategoryTLS)
		}
		if err := c.StartTLS(d.tlsConfig()); err != nil {
			return fail(tlsCategory(err))
		}
	}
	if d.user != "" && d.encryption != EncryptionNone {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fail(CategoryAuth)
		}
		if err := c.Auth(smtp.PlainAuth("", d.user, string(d.password), d.host)); err != nil {
			if connectCategory(err) == CategoryTimeout {
				return fail(CategoryTimeout)
			}
			return fail(CategoryAuth)
		}
	}
	if err := c.Mail(from); err != nil {
		return fail(rejected(err))
	}
	if err := c.Rcpt(to); err != nil {
		return fail(rejected(err))
	}
	w, err := c.Data()
	if err != nil {
		return fail(rejected(err))
	}
	if _, err := w.Write(msg); err != nil {
		return fail(rejected(err))
	}
	if err := w.Close(); err != nil {
		return fail(rejected(err))
	}
	_ = c.Quit()
	return nil
}

func rejected(err error) string {
	if connectCategory(err) == CategoryTimeout {
		return CategoryTimeout
	}
	return CategoryRejected
}
