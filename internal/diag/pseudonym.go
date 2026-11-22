// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"fmt"
	"maps"
	"net"
	"regexp"
	"slices"
	"strings"
)

// Patterns of the values a package blacks out (D-090, plan D5). A hostname
// by pattern ends in a common top-level domain, so module names such as
// panel.cmd stay; the hosts the panel knows go by name.
var (
	emailValue = regexp.MustCompile(`[^\s@"'<>]+@[^\s@"'<>]+\.[A-Za-z]{2,}`)
	fpValue    = regexp.MustCompile(`\b[0-9A-Fa-f]{64}\b|\b(?:[0-9A-Fa-f]{2}:){31}[0-9A-Fa-f]{2}\b`)
	ipv4Value  = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b`)
	ipv6Value  = regexp.MustCompile(`(?i)[0-9a-f]*:[0-9a-f:]*:[0-9a-f]*`)
	hostValue  = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.` +
		`(?:lan|local|localdomain|home|internal|intranet|corp|com|net|org|de|eu|io|dev|cloud|me|info|at|ch|uk|us)\b`)
)

// pseudonyms gives the hosts and IPs of one package stable names (host-1,
// ip-1): equal values get equal names, and the mapping stays out of the
// package (F-111).
type pseudonyms struct {
	known []*regexp.Regexp
	names map[string]string
	hosts int
	ips   int
}

// newPseudonyms starts the names of one package; known are the hosts the
// panel stores (servers, agents), matched as whole words.
func newPseudonyms(known []string) *pseudonyms {
	p := &pseudonyms{names: map[string]string{}}
	set := map[string]bool{}
	for _, k := range known {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			set[k] = true
		}
	}
	// Longest first, so a host is not cut by a shorter one inside it.
	keys := slices.SortedFunc(maps.Keys(set), func(a, b string) int { return len(b) - len(a) })
	for _, k := range keys {
		p.known = append(p.known, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(k)+`\b`))
	}
	return p
}

func (p *pseudonyms) name(kind, v string) string {
	key := kind + strings.ToLower(v)
	if n, ok := p.names[key]; ok {
		return n
	}
	if kind == "ip" {
		p.ips++
		p.names[key] = fmt.Sprintf("ip-%d", p.ips)
	} else {
		p.hosts++
		p.names[key] = fmt.Sprintf("host-%d", p.hosts)
	}
	return p.names[key]
}

// domainAttr is a domain in a text line (domain=x); in JSON the value
// follows the key "domain" in the texts of a line (syslog.Rewrite).
var domainAttr = regexp.MustCompile(`\bdomain=("?)([^\s"]+)`)

// apply blacks out the values of one package: e-mail addresses and
// fingerprints become a mark, IPs and hosts their pseudonym; a domain
// value is a host whatever its top-level domain (D-090).
func (p *pseudonyms) apply(texts []string) []string {
	out := make([]string, len(texts))
	prev := ""
	for i, s := range texts {
		key := prev
		prev = s
		if key == "domain" && s != "" {
			out[i] = p.name("host", s)
			continue
		}
		s = domainAttr.ReplaceAllStringFunc(s, func(c string) string {
			m := domainAttr.FindStringSubmatch(c)
			return "domain=" + m[1] + p.name("host", m[2])
		})
		s = emailValue.ReplaceAllString(s, "[email]")
		s = fpValue.ReplaceAllString(s, "[fingerprint]")
		s = ipv6Value.ReplaceAllStringFunc(s, func(c string) string {
			// A colon next to the address (k:fe80::1, fe80::1: x) is not
			// part of it; it stays as it was.
			pre, ip, post := "", c, ""
			if strings.HasPrefix(ip, ":") && !strings.HasPrefix(ip, "::") {
				pre, ip = ":", ip[1:]
			}
			if strings.HasSuffix(ip, ":") && !strings.HasSuffix(ip, "::") {
				ip, post = ip[:len(ip)-1], ":"
			}
			if (strings.Count(ip, ":") >= 7 || strings.Contains(ip, "::")) && net.ParseIP(ip) != nil {
				return pre + p.name("ip", ip) + post
			}
			return c
		})
		s = ipv4Value.ReplaceAllStringFunc(s, func(c string) string { return p.name("ip", c) })
		for _, k := range p.known {
			s = k.ReplaceAllStringFunc(s, func(c string) string { return p.name("host", c) })
		}
		out[i] = hostValue.ReplaceAllStringFunc(s, func(c string) string { return p.name("host", c) })
	}
	return out
}
