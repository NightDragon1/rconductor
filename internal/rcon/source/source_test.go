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
