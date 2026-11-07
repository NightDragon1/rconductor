// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// Client describes who sent a request, after evaluating trusted proxies
// (NFR-003, Design 2.1).
type Client struct {
	// IP is the client address: the connection peer, or for a trusted
	// proxy the right-most untrusted address in X-Forwarded-For.
	IP netip.Addr
	// HTTPS reports whether the browser reached the UI over HTTPS: the
	// public URL is https, or a trusted proxy sent X-Forwarded-Proto: https.
	HTTPS bool
}

type clientKey struct{}

// ClientOf returns the client of a request handled by NewHandler.
func ClientOf(ctx context.Context) Client {
	c, _ := ctx.Value(clientKey{}).(Client)
	return c
}

// Proxies evaluates forwarding headers, but only from trusted proxies.
type Proxies struct {
	Trusted   []netip.Prefix
	PublicURL *url.URL
}

// HTTPSKnown reports whether the public URL alone guarantees HTTPS. If
// not, HTTPS depends on a trusted proxy per request, and the panel warns at
// start-up.
func (p Proxies) HTTPSKnown() bool { return p.PublicURL != nil && p.PublicURL.Scheme == "https" }

// Trust reports whether a is a trusted proxy.
func (p Proxies) Trust(a netip.Addr) bool { return p.trusted(a) }

func (p Proxies) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, pre := range p.Trusted {
		if pre.Contains(a) {
			return true
		}
	}
	return false
}

// Resolve determines the client of r.
func (p Proxies) Resolve(r *http.Request) Client {
	c := Client{HTTPS: p.HTTPSKnown()}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return c
	}
	c.IP = peer.Unmap()
	if !p.trusted(c.IP) {
		return c
	}

	// Walk X-Forwarded-For from the right: each hop that is a trusted
	// proxy passes on to the address it received from. The first
	// untrusted address is the client; anything left of it is
	// client-controlled and ignored.
	hops := forwardedFor(r.Header.Values("X-Forwarded-For"))
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(hops[i])
		if err != nil {
			break
		}
		c.IP = a.Unmap()
		if !p.trusted(c.IP) {
			break
		}
	}
	if proto := lastValue(r.Header.Values("X-Forwarded-Proto")); strings.EqualFold(proto, "https") {
		c.HTTPS = true
	}
	return c
}

func forwardedFor(values []string) []string {
	var hops []string
	for _, v := range values {
		for h := range strings.SplitSeq(v, ",") {
			h = strings.TrimSpace(h)
			// Some proxies add a port or brackets.
			if ap, err := netip.ParseAddrPort(h); err == nil {
				h = ap.Addr().String()
			}
			hops = append(hops, strings.Trim(h, "[]"))
		}
	}
	return hops
}

// lastValue returns the right-most comma-separated value: the one set by
// the nearest proxy.
func lastValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := strings.Split(values[len(values)-1], ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

func withClient(p Proxies, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey{}, p.Resolve(r))))
	})
}
