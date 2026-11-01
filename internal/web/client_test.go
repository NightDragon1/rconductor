// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
)

func TestResolveClient(t *testing.T) {
	proxies := Proxies{Trusted: []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("fd00::/8"),
	}}
	cases := []struct {
		name, remote string
		xff          []string
		proto        string
		wantIP       string
		wantHTTPS    bool
	}{
		{"direct client", "192.0.2.7:5000", nil, "", "192.0.2.7", false},
		{"headers from untrusted peer are ignored", "192.0.2.7:5000", []string{"198.51.100.1"}, "https", "192.0.2.7", false},
		{"trusted proxy", "10.0.0.2:4000", []string{"198.51.100.1"}, "https", "198.51.100.1", true},
		{"spoofed left part is ignored", "10.0.0.2:4000", []string{"203.0.113.9, 198.51.100.1"}, "http", "198.51.100.1", false},
		{"chain of trusted proxies", "10.0.0.2:4000", []string{"198.51.100.1, 10.1.1.1", "10.2.2.2"}, "https", "198.51.100.1", true},
		{"all hops trusted", "10.0.0.2:4000", []string{"10.9.9.9"}, "", "10.9.9.9", false},
		{"invalid hop stops the walk", "10.0.0.2:4000", []string{"198.51.100.1, garbage"}, "", "10.0.0.2", false},
		{"no header from proxy", "10.0.0.2:4000", nil, "", "10.0.0.2", false},
		{"port and brackets", "[fd00::1]:4000", []string{"[2001:db8::5]:1234"}, "HTTPS", "2001:db8::5", true},
		{"IPv4-mapped peer", "[::ffff:10.0.0.2]:4000", []string{"198.51.100.1"}, "", "198.51.100.1", false},
		{"nearest proto wins", "10.0.0.2:4000", []string{"198.51.100.1"}, "https, http", "198.51.100.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			c := proxies.Resolve(r)
			if c.IP.String() != tc.wantIP || c.HTTPS != tc.wantHTTPS {
				t.Errorf("Resolve = %s https=%v, want %s https=%v", c.IP, c.HTTPS, tc.wantIP, tc.wantHTTPS)
			}
		})
	}
}

func TestHTTPSFromPublicURL(t *testing.T) {
	u, _ := url.Parse("https://rcon.example.net")
	p := Proxies{PublicURL: u}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if !p.HTTPSKnown() || !p.Resolve(r).HTTPS {
		t.Error("https public URL must mean HTTPS")
	}
	u, _ = url.Parse("http://rcon.lan:8080")
	if (Proxies{PublicURL: u}).HTTPSKnown() || (Proxies{}).HTTPSKnown() {
		t.Error("http or empty public URL must not mean HTTPS")
	}
}

func TestClientInContext(t *testing.T) {
	var got Client
	h := NewHandler(Options{UI: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = ClientOf(r.Context()) })})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.7:1"
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got.IP.String() != "192.0.2.7" {
		t.Errorf("client in context = %+v", got)
	}
}
