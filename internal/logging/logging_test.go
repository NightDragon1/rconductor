// SPDX-License-Identifier: AGPL-3.0-or-later

package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
)

func jsonFactory(buf *bytes.Buffer, level slog.Level) *Factory {
	return New(buf, "panel", config.Log{Level: level, Format: "json"})
}

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestJSONEventLine(t *testing.T) {
	var buf bytes.Buffer
	log := jsonFactory(&buf, slog.LevelInfo).Module("db")
	log.Info("migrations applied", Event(DBMigrateDone), slog.String(KeyVersion, "1.0.0"))

	m := decodeLines(t, &buf)[0]
	if m[KeyComp] != "panel.db" || m[KeyEvent] != "db.migrate.done" || m[KeyLevel] != "info" {
		t.Errorf("unexpected fields: %v", m)
	}
	ts, _ := m[KeyTS].(string)
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`).MatchString(ts) {
		t.Errorf("ts %q is not UTC RFC 3339 with milliseconds", ts)
	}
	if _, ok := m["time"]; ok {
		t.Error("default time key still present")
	}
}

func TestRootComp(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "agent", config.Log{Level: slog.LevelInfo, Format: "json"}).Root().Info("x", Event(AppStart))
	if m := decodeLines(t, &buf)[0]; m[KeyComp] != "agent" {
		t.Errorf("comp = %v, want agent", m[KeyComp])
	}
}

func TestLevelNames(t *testing.T) {
	var buf bytes.Buffer
	log := jsonFactory(&buf, slog.LevelDebug).Root()
	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
	want := []string{"debug", "info", "warn", "error"}
	for i, m := range decodeLines(t, &buf) {
		if m[KeyLevel] != want[i] {
			t.Errorf("line %d level = %v, want %s", i, m[KeyLevel], want[i])
		}
	}
}

func TestDurationOnlyOnDebug(t *testing.T) {
	for _, tc := range []struct {
		level slog.Level
		want  bool
	}{{slog.LevelDebug, true}, {slog.LevelInfo, false}} {
		var buf bytes.Buffer
		jsonFactory(&buf, tc.level).Root().Warn("slow", Duration(1500*time.Millisecond))
		_, got := decodeLines(t, &buf)[0][KeyDuration]
		if got != tc.want {
			t.Errorf("level %v: duration_ms present = %v, want %v", tc.level, got, tc.want)
		}
	}
}

// TestNoSecretsInOutput covers the redaction paths of SEC-012 for both formats.
func TestNoSecretsInOutput(t *testing.T) {
	const pw = "hunter2-very-secret"
	for _, format := range []string{"json", "text"} {
		var buf bytes.Buffer
		log := New(&buf, "panel", config.Log{Level: slog.LevelDebug, Format: format}).Root()

		log.Debug("by key", slog.String("password", pw), slog.String("RCON_Password", pw),
			slog.String("pairing_token", pw), slog.String("session_id", pw), slog.Any("token", []byte(pw)))
		log.Debug("nested", slog.Group("req", slog.String("authorization", "Bearer "+pw)))
		log.Debug("typed", slog.Any("value", Secret(pw)), slog.String("fmt", fmt.Sprintf("%v %s %#v", Secret(pw), Secret(pw), Secret(pw))))
		log.Debug("url postgres://rcd:"+pw+"@db:5432/x", slog.String("target", "postgres://rcd:"+pw+"@db/x"))
		log.Error("failed", slog.Any("err", errors.New("dial postgres://rcd:"+pw+"@db: refused")))
		log.With(slog.String("api_token", pw)).Info("with")

		if out := buf.String(); strings.Contains(out, pw) {
			t.Errorf("%s output leaks secret:\n%s", format, out)
		}
		if !strings.Contains(buf.String(), Redacted) {
			t.Errorf("%s output has no redaction marker", format)
		}
	}
}

func TestSecretMarshalJSON(t *testing.T) {
	b, err := json.Marshal(struct{ P Secret }{P: "hunter2"})
	if err != nil || strings.Contains(string(b), "hunter2") {
		t.Errorf("json.Marshal leaks secret: %s %v", b, err)
	}
	if Secret("hunter2").Reveal() != "hunter2" {
		t.Error("Reveal lost value")
	}
}

func TestSessionRef(t *testing.T) {
	ref := SessionRef("session-123")
	if len(ref) != 8 || strings.Contains(ref, "session") {
		t.Errorf("SessionRef = %q", ref)
	}
	if ref != SessionRef("session-123") || ref == SessionRef("session-124") {
		t.Error("SessionRef not deterministic or not distinct")
	}
}

func TestCodesUniqueAndWellFormed(t *testing.T) {
	re := regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)
	seen := map[Code]bool{}
	for _, c := range AllCodes {
		if seen[c] {
			t.Errorf("duplicate code %s", c)
		}
		seen[c] = true
		if !re.MatchString(string(c)) {
			t.Errorf("malformed code %s", c)
		}
	}
	// Design 8.3 (revision 2.2) lists 46; agent.disconnected comes with
	// revision 2.3 candidate 5, logs.tail with revision 2.4 candidate 20,
	// audit.entry_lost with the addendum to revision 2.7, and
	// rcon.connection_test with revision 2.8.
	if len(AllCodes) != 50 {
		t.Errorf("AllCodes has %d entries, want 46 of Design 8.3 plus agent.disconnected, logs.tail, audit.entry_lost and rcon.connection_test", len(AllCodes))
	}
}
