// SPDX-License-Identifier: AGPL-3.0-or-later

package source

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

func start(t *testing.T, style rcontest.MarkerStyle, h func(string) string) *rcontest.Server {
	t.Helper()
	s, err := rcontest.Start("secret", style, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func dial(t *testing.T, s *rcontest.Server) rcon.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Adapter{}.Dial(ctx, s.Addr, "secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestAuth(t *testing.T) {
	s := start(t, rcontest.Minecraft, nil)
	dial(t, s)
	_, err := Adapter{}.Dial(context.Background(), s.Addr, logging.Secret("wrong"))
	if !errors.Is(err, rcon.ErrAuth) {
		t.Errorf("wrong password: %v", err)
	}
}

// TestLongResponse covers PROT-001: answers over 4 KB arrive complete and
// unchanged, for both marker styles; a late second marker answer of
// Source servers does not mix into the next command.
func TestLongResponse(t *testing.T) {
	long := strings.Repeat("0123456789abcdef", 1500) // 24 000 bytes, 6 packets
	for name, style := range map[string]rcontest.MarkerStyle{"minecraft": rcontest.Minecraft, "source": rcontest.SourceEngine} {
		t.Run(name, func(t *testing.T) {
			s := start(t, style, func(cmd string) string {
				if cmd == "dump" {
					return long
				}
				return "echo " + cmd
			})
			c := dial(t, s)
			ctx := context.Background()
			got, err := c.Execute(ctx, "dump")
			if err != nil || got != long {
				t.Fatalf("long answer: %d bytes, %v", len(got), err)
			}
			for _, cmd := range []string{"list", "say hi", "list"} {
				if got, err := c.Execute(ctx, cmd); err != nil || got != "echo "+cmd {
					t.Errorf("%s = %q, %v", cmd, got, err)
				}
			}
			if got, _ := c.Execute(ctx, "nothing"); got != "echo nothing" {
				t.Errorf("after several markers: %q", got)
			}
		})
	}
}

func TestEmptyAnswer(t *testing.T) {
	s := start(t, rcontest.Minecraft, func(string) string { return "" })
	if got, err := dial(t, s).Execute(context.Background(), "save-all"); err != nil || got != "" {
		t.Errorf("empty answer = %q, %v", got, err)
	}
}

func TestInvalidCommands(t *testing.T) {
	c := dial(t, start(t, rcontest.Minecraft, nil))
	for _, cmd := range []string{"", "a\x00b", strings.Repeat("x", MaxCommand+1)} {
		if _, err := c.Execute(context.Background(), cmd); err == nil {
			t.Errorf("command %.10q accepted", cmd)
		}
	}
}

func TestResponseLimit(t *testing.T) {
	huge := strings.Repeat("x", MaxResponse+4096)
	c := dial(t, start(t, rcontest.Minecraft, func(string) string { return huge }))
	if _, err := c.Execute(context.Background(), "x"); !errors.Is(err, rcon.ErrTooLarge) {
		t.Errorf("oversized answer: %v", err)
	}
}

// TestOversizedPacket: a hostile server announcing a huge packet is
// refused without reading it.
func TestOversizedPacket(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		buf := make([]byte, 64)
		_, _ = c.Read(buf)
		_, _ = c.Write(binary.LittleEndian.AppendUint32(nil, 1<<30))
		time.Sleep(time.Second)
	}()
	if _, err := (Adapter{}).Dial(context.Background(), ln.Addr().String(), "x"); !errors.Is(err, rcon.ErrTooLarge) {
		t.Errorf("huge packet: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	c := dial(t, start(t, rcontest.Minecraft, func(string) string { time.Sleep(time.Second); return "late" }))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.Execute(ctx, "slow"); err == nil {
		t.Error("no timeout")
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Error("timeout not applied")
	}
}

func TestAlive(t *testing.T) {
	s := start(t, rcontest.SourceEngine, nil)
	c := dial(t, s).(*Conn)
	if err := c.Alive(); err != nil {
		t.Fatalf("alive: %v", err)
	}
	// A late second marker answer stays buffered and does no harm.
	if _, err := c.Execute(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := c.Alive(); err != nil {
		t.Errorf("alive with buffered data: %v", err)
	}
	if got, err := c.Execute(context.Background(), "b"); err != nil || got != "b" {
		t.Errorf("after probe: %q %v", got, err)
	}
	s.DropConnections()
	time.Sleep(50 * time.Millisecond)
	if err := c.Alive(); err == nil {
		t.Error("closed connection reported alive")
	}
}

// TestPing covers the idle probe of Design 3.8: both server styles answer
// it and commands work afterwards; a silent server (half-open connection)
// lets it fail when the context ends, where Alive still sees nothing wrong.
func TestPing(t *testing.T) {
	for name, style := range map[string]rcontest.MarkerStyle{"minecraft": rcontest.Minecraft, "source": rcontest.SourceEngine} {
		s := start(t, style, nil)
		c := dial(t, s).(*Conn)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := c.Ping(ctx); err != nil {
			t.Errorf("%s: ping: %v", name, err)
		}
		cancel()
		if got, err := c.Execute(context.Background(), "a"); err != nil || got != "a" {
			t.Errorf("%s: after ping: %q %v", name, got, err)
		}
		s.Freeze()
		if err := c.Alive(); err != nil {
			t.Errorf("%s: a read alone noticed the silence: %v", name, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
		if err := c.Ping(ctx); err == nil {
			t.Errorf("%s: silent server answered the ping", name)
		}
		cancel()
	}
}

func TestKeepAlive(t *testing.T) {
	if !KeepAlive.Enable || KeepAlive.Idle != 10*time.Second || KeepAlive.Interval != 3*time.Second || KeepAlive.Count != 3 {
		t.Errorf("keepalive = %+v, want idle 10 s, interval 3 s, 3 probes (Design 3.8)", KeepAlive)
	}
}

func dialMarker(t *testing.T, s *rcontest.Server, marker string) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Adapter{}.DialEndpoint(ctx, rcon.Endpoint{Addr: s.Addr, Secret: "secret", EndMarker: marker})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c.(*Conn)
}

// TestO8FactorioEndMarker covers PROT-001 for servers that refuse packets
// of type 0 and never answer them, like Factorio: with the end marker
// none every command is answered, long answers in one packet too, and
// neither a command nor the idle probe sends a packet the server refuses.
// It fails on the code before the end marker could be chosen: the command
// waits for an answer to its marker until the timeout.
func TestO8FactorioEndMarker(t *testing.T) {
	long := strings.Repeat("0123456789abcdef", 1000) // 16 000 bytes in one packet
	s := start(t, rcontest.Factorio, func(cmd string) string {
		switch cmd {
		case "/dump":
			return long
		case "/silent-command x=1":
			return ""
		}
		return "echo " + cmd
	})
	c := dialMarker(t, s, EndMarkerNone)
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		got, err := c.Execute(ctx, "/version")
		cancel()
		if err != nil || got != "echo /version" {
			t.Fatalf("command = %q, %v", got, err)
		}
	}
	if got, err := c.Execute(context.Background(), "/dump"); err != nil || got != long {
		t.Errorf("long answer: %d bytes, %v", len(got), err)
	}
	if got, err := c.Execute(context.Background(), "/silent-command x=1"); err != nil || got != "" {
		t.Errorf("empty answer = %q, %v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Errorf("ping: %v", err)
	}
	if err := c.Alive(); err != nil {
		t.Errorf("alive: %v", err)
	}
	if got, err := c.Execute(context.Background(), "/players"); err != nil || got != "echo /players" {
		t.Errorf("after probe = %q, %v", got, err)
	}
	if n := s.Invalid(); n != 0 {
		t.Errorf("server refused %d packets as invalid message type 0", n)
	}
	if n := s.Connections(); n != 1 {
		t.Errorf("%d connections, want 1", n)
	}
}

// TestO8DefaultEndMarker: without an end marker in the profile and with
// empty_response the client keeps the marker, so a Factorio-like server
// is not answered (the behavior profiles without the field keep), and a
// split answer is complete.
func TestO8DefaultEndMarker(t *testing.T) {
	for _, m := range []string{"", EndMarkerEmptyResponse} {
		c := dialMarker(t, start(t, rcontest.Factorio, nil), m)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		if _, err := c.Execute(ctx, "/version"); err == nil {
			t.Errorf("%q: the marker was not sent", m)
		}
		cancel()
		long := strings.Repeat("x", 9000)
		c = dialMarker(t, start(t, rcontest.SourceEngine, func(string) string { return long }), m)
		if got, err := c.Execute(context.Background(), "dump"); err != nil || got != long {
			t.Errorf("%q: split answer %d bytes, %v", m, len(got), err)
		}
	}
}

// TestO8NoneSplitAnswer documents the limit of the end marker none: a
// server that splits an answer gives only its first packet, and the rest,
// with the old packet id, never mixes into the next command.
func TestO8NoneSplitAnswer(t *testing.T) {
	long := strings.Repeat("y", 9000) // 3 packets
	s := start(t, rcontest.SourceEngine, func(cmd string) string {
		if cmd == "dump" {
			return long
		}
		return "echo " + cmd
	})
	c := dialMarker(t, s, EndMarkerNone)
	if got, err := c.Execute(context.Background(), "dump"); err != nil || got != long[:4096] {
		t.Fatalf("first packet: %d bytes, %v", len(got), err)
	}
	for _, cmd := range []string{"a", "b"} {
		if got, err := c.Execute(context.Background(), cmd); err != nil || got != "echo "+cmd {
			t.Errorf("%s = %q, %v", cmd, got, err)
		}
	}
}

// TestO8UnknownEndMarker: an end marker the adapter does not know is
// refused before anything is sent.
func TestO8UnknownEndMarker(t *testing.T) {
	s := start(t, rcontest.Factorio, nil)
	_, err := Adapter{}.DialEndpoint(context.Background(), rcon.Endpoint{Addr: s.Addr, Secret: "secret", EndMarker: "marker"})
	if err == nil || s.Connections() != 0 {
		t.Errorf("unknown end marker: %v, %d connections", err, s.Connections())
	}
	for _, m := range EndMarkers() {
		if !KnownEndMarker(m) {
			t.Errorf("%s not known", m)
		}
	}
}

// TestO8EndOfAnswerWithoutMarker covers PROT-001 with end marker none
// (D-079): the end of an answer is the first packet with the command's
// id, so a normal, a long single-packet and an empty answer return at
// once, far below the deadline, without waiting for anything else.
func TestO8EndOfAnswerWithoutMarker(t *testing.T) {
	long := strings.Repeat("0123456789abcdef", 4000) // 64 000 bytes, one packet
	s := start(t, rcontest.Factorio, func(cmd string) string {
		switch cmd {
		case "/dump":
			return long
		case "/silent-command x=1":
			return ""
		}
		return "Version: 2.1.24"
	})
	c := dialMarker(t, s, EndMarkerNone)
	const deadline = 5 * time.Second
	for cmd, want := range map[string]string{"/version": "Version: 2.1.24", "/dump": long, "/silent-command x=1": ""} {
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		begin := time.Now()
		got, err := c.Execute(ctx, cmd)
		took := time.Since(begin)
		cancel()
		if err != nil || got != want {
			t.Errorf("%s: %d bytes, %v", cmd, len(got), err)
		}
		if took > deadline/5 {
			t.Errorf("%s took %v, want far below the deadline %v", cmd, took, deadline)
		}
	}
	if s.Invalid() != 0 || s.Connections() != 1 {
		t.Errorf("%d refused packets, %d connections", s.Invalid(), s.Connections())
	}
}

// TestO8ChatInputNoAnswer: input without a leading "/" is chat for
// Factorio and gets no packet at all, so the protocol gives nothing to end
// on: the command ends with a timeout error at its deadline, never later,
// and the connection stays for the next command.
func TestO8ChatInputNoAnswer(t *testing.T) {
	s := start(t, rcontest.Factorio, func(string) string { return "ok" })
	c := dialMarker(t, s, EndMarkerNone)
	const deadline = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	begin := time.Now()
	got, err := c.Execute(ctx, "hello")
	took := time.Since(begin)
	var ne net.Error
	if got != "" || !errors.As(err, &ne) || !ne.Timeout() {
		t.Errorf("chat input = %q, %v, want a timeout", got, err)
	}
	if took > deadline+time.Second {
		t.Errorf("took %v, deadline %v", took, deadline)
	}
	if got, err := c.Execute(context.Background(), "/players"); err != nil || got != "ok" {
		t.Errorf("next command = %q, %v", got, err)
	}
	if s.Chat() != 1 || s.Connections() != 1 || s.Invalid() != 0 {
		t.Errorf("chat %d, connections %d, refused %d", s.Chat(), s.Connections(), s.Invalid())
	}
}
