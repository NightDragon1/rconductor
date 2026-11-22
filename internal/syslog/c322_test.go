// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package syslog

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// The masking of the panel gives the patterns of a server to the sink.
var _ serverMasker = logmask.Always{}

// byServer masks the word of a server's profile pattern only where the
// sink hands that server's patterns over.
type byServer struct {
	server string
	asked  []string
}

func (b *byServer) Lines(lines []string, patterns [][]string) ([]string, int) {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l
		for _, ps := range patterns {
			for _, p := range ps {
				out[i] = strings.ReplaceAll(out[i], p, "•••")
			}
		}
	}
	return out, 0
}

func (b *byServer) PatternsFor(_ context.Context, serverID string) [][]string {
	b.asked = append(b.asked, serverID)
	if serverID == b.server {
		return [][]string{{"rcon-pass-77"}}
	}
	return nil
}

// TestC322ProfilePatternsOfTheServer (D-088 b, D-107): an entry about a
// server is masked with the profile patterns of that server; an entry
// without a server, or with another one, is not; a server value that is no
// UUID asks for nothing.
func TestC322ProfilePatternsOfTheServer(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	const server = "00000000-0000-4000-8000-000000000322"
	var out bytes.Buffer
	logs := logging.New(&out, "panel", config.Log{Format: "json"})
	sink := NewSink()
	logs.Also(sink)
	log := logs.Module("test")
	log.Warn("answer rcon-pass-77", slog.String(logging.KeyServer, strings.ToUpper(server)))
	log.Warn("answer rcon-pass-77 without a server")
	log.Warn("answer rcon-pass-77 of another", slog.String(logging.KeyServer, "00000000-0000-4000-8000-000000000001"))
	log.Warn("answer rcon-pass-77 no uuid", slog.String(logging.KeyServer, "not-a-uuid"))
	m := &byServer{server: server}
	sink.drain(ctx, st, m, log)
	rows, err := st.DB.QueryContext(ctx, `SELECT text FROM system_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var texts []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		texts = append(texts, s)
	}
	if len(texts) != 4 || strings.Contains(texts[0], "rcon-pass-77") {
		t.Fatalf("texts = %q", texts)
	}
	for _, s := range texts[1:] {
		if !strings.Contains(s, "rcon-pass-77") {
			t.Errorf("masked with a pattern of another server: %s", s)
		}
	}
	if len(m.asked) != 2 || m.asked[0] != server {
		t.Errorf("patterns asked for %q", m.asked)
	}
}
