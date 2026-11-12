// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
)

// packet is one Source RCON packet on the wire.
func packet(id, typ int32, body string) string {
	b := binary.LittleEndian.AppendUint32(nil, uint32(10+len(body))) //nolint:gosec // test data
	b = binary.LittleEndian.AppendUint32(b, uint32(id))              //nolint:gosec // wire format
	b = binary.LittleEndian.AppendUint32(b, uint32(typ))             //nolint:gosec // wire format
	return string(append(append(b, body...), 0, 0))
}

// TestC71OversizedAnswerSharedConnection covers PROT-001 and PROT-002 on
// the one connection that all users of a server share: user A gets an
// answer over the packet limit whose content looks like packets; the
// commands of user B that follow get their own answers or a clear error,
// never a part of A's answer. Both end markers are checked.
func TestC71OversizedAnswerSharedConnection(t *testing.T) {
	for _, marker := range []string{source.EndMarkerEmptyResponse, source.EndMarkerNone} {
		t.Run(marker, func(t *testing.T) {
			// Packet ids on the first connection: auth 1, then two per
			// command (command and marker), so B's third command has 8.
			inner := packet(8, 0, "not B's answer") + packet(9, 0, "") + packet(6, 0, "not B's answer") + packet(4, 0, "not B's answer")
			huge := inner + strings.Repeat("z", source.MaxPacketBody)
			srv, err := rcontest.Start("secret", rcontest.SourceEngine, func(cmd string) string {
				if cmd == "a-huge" {
					return huge
				}
				return "answer to " + cmd
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(srv.Close)
			srv.SinglePackets()
			src := &fakeSource{targets: map[string]Target{
				"s1": {ID: "s1", Name: "game", Protocol: "source-rcon", Addr: srv.Addr, Password: "secret", Usable: true, EndMarker: marker},
			}}
			// No idle checks: only the commands read the connection.
			m := New(rcon.Registry{"source-rcon": source.Adapter{}}, src, logging.New(&lockedBuf{}, "panel", config.Log{Format: "json"}).Module("rcon"), Options{
				ProbeInterval: time.Hour, MaxBackoff: 200 * time.Millisecond, IdleProbe: time.Hour, ProbeTimeout: time.Second,
			})
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(func() { cancel(); m.Wait() })
			if err := m.Start(ctx); err != nil {
				t.Fatal(err)
			}
			(&fixture{srv: srv, m: m}).waitStatus(t, Connected)

			if got, err := m.Execute(context.Background(), "s1", "a-huge"); err == nil || got != "" {
				t.Errorf("user A: %d bytes, %v, want an error", len(got), err)
			}
			own := 0
			deadline := time.Now().Add(10 * time.Second)
			for i := 1; own < 3; i++ {
				if time.Now().After(deadline) {
					t.Fatalf("user B got %d own answers within 10 s", own)
				}
				cmd := "b-" + strings.Repeat("x", i%3+1)
				cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
				got, err := m.Execute(cctx, "s1", cmd)
				ccancel()
				switch {
				case err == nil && got == "answer to "+cmd:
					own++
				case err == nil:
					t.Fatalf("user B's %s got %.40q, not its own answer", cmd, got)
				default:
					// A clear error while the connection is built anew.
					time.Sleep(10 * time.Millisecond)
				}
			}
		})
	}
}
