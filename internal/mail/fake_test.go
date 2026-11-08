// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// certFor makes a certificate for localhost and 127.0.0.1 and a pool that
// trusts it.
func certFor(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// fakeSMTP is a small SMTP server: it offers STARTTLS when starttls is
// set, speaks TLS from the start when ssl is set, checks AUTH PLAIN
// against user and password, and records the commands and messages.
type fakeSMTP struct {
	t        *testing.T
	ln       net.Listener
	cert     tls.Certificate
	starttls bool
	ssl      bool
	silent   bool // answers nothing: a time-out
	rejectTo bool // refuses every recipient
	user     string
	password string

	mu       sync.Mutex
	commands []string
	messages []string
	plain    []string // commands seen before TLS
}

func startSMTP(t *testing.T, cert tls.Certificate, set func(*fakeSMTP)) *fakeSMTP {
	t.Helper()
	f := &fakeSMTP{t: t, cert: cert, user: "mailer", password: "smtp-Secret-4711"}
	set(f)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if f.ssl {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	}
	f.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) seen() ([]string, []string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...), append([]string(nil), f.messages...), append([]string(nil), f.plain...)
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if f.silent {
		_, _ = bufio.NewReader(c).ReadString('\n')
		return
	}
	r, w := bufio.NewReader(c), bufio.NewWriter(c)
	say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
	secure := f.ssl
	say("220 localhost ESMTP fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		f.mu.Lock()
		f.commands = append(f.commands, verb)
		if !secure {
			f.plain = append(f.plain, verb)
		}
		f.mu.Unlock()
		switch verb {
		case "EHLO", "HELO":
			ext := []string{"250-localhost"}
			if f.starttls && !secure {
				ext = append(ext, "250-STARTTLS")
			}
			ext = append(ext, "250 AUTH PLAIN")
			for _, e := range ext {
				_, _ = w.WriteString(e + "\r\n")
			}
			_ = w.Flush()
		case "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}})
			if err := tc.Handshake(); err != nil {
				return
			}
			c, secure = tc, true
			r, w = bufio.NewReader(tc), bufio.NewWriter(tc)
		case "AUTH":
			parts := strings.Fields(line)
			raw, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			if string(raw) == "\x00"+f.user+"\x00"+f.password {
				say("235 ok")
			} else {
				say("535 authentication failed")
			}
		case "MAIL":
			say("250 ok")
		case "RCPT":
			if f.rejectTo {
				say("550 no such user here")
			} else {
				say("250 ok")
			}
		case "DATA":
			say("354 go on")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.messages = append(f.messages, b.String())
			f.mu.Unlock()
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}
