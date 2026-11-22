// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
)

func TestRoundTrip(t *testing.T) {
	data, err := Encode(TypeContainerAction, "", ContainerAction{Container: "mc", Action: ActionRestart})
	if err != nil {
		t.Fatal(err)
	}
	e, known, err := Decode(data, MaxPanelMessage, FromPanel)
	if err != nil || !known || e.Type != TypeContainerAction || e.ID == "" || e.TS.IsZero() {
		t.Fatalf("decode = %+v %v %v", e, known, err)
	}
	a, err := Payload[ContainerAction](e)
	if err != nil || a.Container != "mc" || a.Action != ActionRestart {
		t.Errorf("payload = %+v %v", a, err)
	}
	r, _ := Encode(TypeResult, e.ID, Result{Code: CodeOK})
	if e, _, _ := Decode(r, MaxAgentMessage, FromAgent); e.Ref == "" {
		t.Error("answer without ref")
	}
}

// TestDecodeRefuses covers the protocol part of SEC-007 and AGT-004:
// oversized and broken messages are protocol errors; a type the sender
// may not send is reported as unknown, so the agent answers unsupported.
func TestDecodeRefuses(t *testing.T) {
	big := `{"type":"metrics","id":"x","payload":"` + strings.Repeat("a", MaxAgentMessage) + `"}`
	for name, data := range map[string]string{
		"too large": big,
		"not json":  "hello",
		"no type":   `{"id":"1"}`,
		"no id":     `{"type":"hello"}`,
	} {
		if _, _, err := Decode([]byte(data), MaxAgentMessage, FromAgent); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, typ := range []string{"container.create", "container.exec", "container.remove", "image.pull", "volume.remove", TypeHello} {
		data, _ := Encode(typ, "", map[string]string{})
		if _, known, err := Decode(data, MaxPanelMessage, FromPanel); err != nil || known {
			t.Errorf("%s from the panel: known %v, err %v", typ, known, err)
		}
	}
	if _, err := Payload[ContainerAction](Envelope{Type: TypeContainerAction}); !errors.Is(err, ErrProtocol) {
		t.Errorf("missing payload: %v", err)
	}
}

// TestSameMajor covers the version rule of AGT-013.
func TestSameMajor(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"1.2", "1.0", true},
		{"v1.4", "1.0-rc1", true},
		{"0.0.0-dev", "0.3.1", true},
		{"1.0", "2.0", false},
		{"0.9", "0.8", true},
		{"0.9", "1.0", false},
		{"dev", "dev", false},
		{"", "1.0", false},
	} {
		if got := SameMajor(tc.a, tc.b); got != tc.want {
			t.Errorf("SameMajor(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

// TestLimitLines covers revision 2.3 candidate 2: at most 500 lines,
// 8 KiB per line and 512 KiB in all, newest lines kept, cut reported.
func TestLimitLines(t *testing.T) {
	lines := make([]string, 600)
	for i := range lines {
		lines[i] = "line\x1b[0m"
	}
	lines[599] = "last"
	out, cut := LimitLines(lines)
	if len(out) != MaxLogLines || !cut || out[len(out)-1] != "last" || out[0] != "line[0m" {
		t.Errorf("lines: %d %v %q", len(out), cut, out[len(out)-1])
	}
	long := []string{strings.Repeat("x", MaxLogLine+10)}
	if out, cut := LimitLines(long); len(out[0]) != MaxLogLine || !cut {
		t.Errorf("long line: %d %v", len(out[0]), cut)
	}
	many := make([]string, 100)
	for i := range many {
		many[i] = strings.Repeat("y", MaxLogLine)
	}
	out, cut = LimitLines(many)
	total := 0
	for _, l := range out {
		total += len(l)
	}
	if total > MaxLogBytes || !cut || len(out) != MaxLogBytes/MaxLogLine {
		t.Errorf("bytes: %d in %d lines, cut %v", total, len(out), cut)
	}
	if out, cut := LimitLines([]string{"a", "b"}); len(out) != 2 || cut {
		t.Errorf("short: %v %v", out, cut)
	}
}

// TestCleanHelloVersion covers the version of a hostile agent (SEC-006):
// markup stays text for the output to escape, direction overrides go, a
// version over 64 characters is unknown, not cut (D-080).
func TestCleanHelloVersion(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"markup":   {"<script>alert(1)</script>", "<script>alert(1)</script>"},
		"override": {"1.0\u202E.0", "1.0.0"},
		"64":       {strings.Repeat("9", 64), strings.Repeat("9", 64)},
		"too long": {strings.Repeat("9", 65), "unknown"},
		"combined": {"\u202E<b>" + strings.Repeat("x", 100), "unknown"},
		"cleaned":  {"\u202E" + strings.Repeat("9", 64), strings.Repeat("9", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			h := Hello{Name: "host", Version: tc.in, Backend: "docker"}
			if err := h.Clean(); err != nil {
				t.Fatal(err)
			}
			if h.Version != tc.want {
				t.Errorf("version = %q, want %q", h.Version, tc.want)
			}
		})
	}
}

func TestCleanHello(t *testing.T) {
	h := Hello{Name: "host\u202e1", Version: "1.0.0", Backend: "docker", Capabilities: []string{CapMetrics}}
	if err := h.Clean(); err != nil || h.Name != "host1" {
		t.Errorf("clean = %+v %v", h, err)
	}
	h.Capabilities = make([]string, 40)
	if err := h.Clean(); !errors.Is(err, ErrProtocol) {
		t.Errorf("too many capabilities: %v", err)
	}
}

// TestCleanDockerHost covers the Docker host of a hostile agent (SEC-006,
// AGT-014): the name loses control and direction characters and is bounded,
// the address stays only as a parsed IP address without zone, and a hello
// without it stays without.
func TestCleanDockerHost(t *testing.T) {
	for name, tc := range map[string]struct{ in, want DockerHost }{
		"plain":     {DockerHost{Name: "nas", IP: "192.168.1.5"}, DockerHost{Name: "nas", IP: "192.168.1.5"}},
		"override":  {DockerHost{Name: "n\u202eas\x00"}, DockerHost{Name: "nas"}},
		"long name": {DockerHost{Name: strings.Repeat("h", 300)}, DockerHost{Name: strings.Repeat("h", MaxHostName)}},
		"ipv6":      {DockerHost{IP: "fd00::1"}, DockerHost{IP: "fd00::1"}},
		"mapped":    {DockerHost{IP: "::ffff:10.0.0.1"}, DockerHost{IP: "10.0.0.1"}},
		"zone":      {DockerHost{IP: "fd00::1%<b>eth0</b>"}, DockerHost{IP: "fd00::1"}},
		// No other host reaches these (D-084).
		"loopback":     {DockerHost{IP: "127.0.1.1"}, DockerHost{}},
		"loopback v6":  {DockerHost{IP: "::1"}, DockerHost{}},
		"link-local":   {DockerHost{IP: "169.254.10.1"}, DockerHost{}},
		"link-local6":  {DockerHost{IP: "fe80::1%eth0"}, DockerHost{}},
		"unspecified":  {DockerHost{IP: "0.0.0.0"}, DockerHost{}},
		"multicast":    {DockerHost{IP: "224.0.0.1"}, DockerHost{}},
		"this network": {DockerHost{IP: "0.1.2.3"}, DockerHost{}},
		"broadcast":    {DockerHost{IP: "255.255.255.255"}, DockerHost{}},
		"with port":    {DockerHost{IP: "10.0.0.1:2377"}, DockerHost{}},
		"not an ip":    {DockerHost{IP: "<script>"}, DockerHost{}},
		"long junk":    {DockerHost{IP: strings.Repeat("1", 10000)}, DockerHost{}},
	} {
		t.Run(name, func(t *testing.T) {
			h := Hello{Name: "host", Version: "1.0.0", Backend: "docker", DockerHost: &tc.in}
			if err := h.Clean(); err != nil {
				t.Fatal(err)
			}
			if *h.DockerHost != tc.want {
				t.Errorf("docker host = %+v, want %+v", *h.DockerHost, tc.want)
			}
		})
	}
	h := Hello{Name: "host", Version: "1.0.0", Backend: "docker"}
	if err := h.Clean(); err != nil || h.DockerHost != nil {
		t.Errorf("without docker host: %+v %v", h, err)
	}
	b, _ := json.Marshal(h)
	if strings.Contains(string(b), "docker_host") {
		t.Errorf("hello without docker host = %s", b)
	}
}

func TestPairingToken(t *testing.T) {
	pt, err := NewPairingToken("panel:8443", "SHA256:abc")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePairingToken(" " + pt.Encode() + "\n")
	if err != nil || got != pt {
		t.Fatalf("round trip = %+v %v", got, err)
	}
	if strings.Contains(pt.String(), pt.Secret) || strings.Contains(fmt.Sprint(pt), pt.Secret) {
		t.Error("printing the token reveals the secret")
	}
	for _, bad := range []string{"", "rcd2.x", "rcd1.!!", "rcd1." + strings.Repeat("A", 2000)} {
		if _, err := ParsePairingToken(bad); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if len(SecretHash(pt.Secret)) != 32 {
		t.Error("hash is not SHA-256")
	}
}

// TestC210HelloVersionLengthBeforeCut: a version over the bound is unknown
// also when a space sits at the cut (D-080, SEC-006).
func TestC210HelloVersionLengthBeforeCut(t *testing.T) {
	h := Hello{Version: strings.Repeat("1", MaxHelloVersion) + " x"}
	_ = h.Clean() // the version alone is under test
	if h.Version != buildinfo.Unknown {
		t.Errorf("version = %q, want unknown", h.Version)
	}
}
