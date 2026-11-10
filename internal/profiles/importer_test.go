// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

type importFixture struct {
	st   *store.Store
	im   *Importer
	dir  string
	logs *syncBuf
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newImportFixture(t *testing.T) *importFixture {
	t.Helper()
	st := storetest.New(t)
	if err := EnsureDefaults(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "profiles")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	logs := &syncBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("profiles")
	builtin := fstest.MapFS{"minecraft-java.yaml": {Data: []byte(design74)}, "README.txt": {Data: []byte("x")}}
	return &importFixture{st: st, dir: dir, logs: logs, im: NewImporter(st, log, builtin, dir)}
}

func (f *importFixture) profile(t *testing.T, slug string) (name, source string, overridden int, lastErr sql.NullString, commands int) {
	t.Helper()
	ctx := context.Background()
	err := f.st.DB.QueryRowContext(ctx, `SELECT name, source_file, overridden, last_error FROM game_profiles WHERE slug = ?`, slug).
		Scan(&name, &source, &overridden, &lastErr)
	if err != nil {
		return "", "", 0, sql.NullString{}, 0
	}
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM catalog_commands c JOIN game_profiles p ON p.id = c.profile_id WHERE p.slug = ?`, slug).Scan(&commands)
	return name, source, overridden, lastErr, commands
}

func (f *importFixture) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func custom(id, name string) string {
	return "schema: 1\nid: " + id + "\nname: " + name + "\nprotocol: { type: source-rcon, default_port: 27015 }\ncatalog:\n  - { path: status, description: a command }\n"
}

// TestSync covers CAT-001 and CAT-003: shipped profiles are imported with
// their catalog; an unchanged file is not imported again.
func TestSync(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	name, source, _, _, n := f.profile(t, "minecraft-java")
	if name != "Minecraft (Java Edition)" || source != "builtin:minecraft-java.yaml" || n != 5 {
		t.Errorf("imported %q from %q with %d commands", name, source, n)
	}
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if c := logtest.WaitCount(t, f.logs, `"event":"profile.imported"`, 1); c != 1 {
		t.Errorf("profile.imported %d times, want 1", c)
	}
}

func TestOverrideAndFallback(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	_ = f.im.Sync(ctx)
	f.write(t, "mc.yaml", custom("minecraft-java", "Our Minecraft"))
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	name, _, overridden, _, n := f.profile(t, "minecraft-java")
	if name != "Our Minecraft" || overridden != 1 || n != 1 {
		t.Errorf("override: %q overridden=%d commands=%d", name, overridden, n)
	}
	logtest.Wait(t, f.logs, `"event":"profile.overridden"`)
	// Removing the custom file brings the shipped profile back.
	_ = os.Remove(filepath.Join(f.dir, "mc.yaml"))
	_ = f.im.Sync(ctx)
	if name, _, overridden, _, _ := f.profile(t, "minecraft-java"); name != "Minecraft (Java Edition)" || overridden != 0 {
		t.Errorf("fallback: %q overridden=%d", name, overridden)
	}
}

// TestBrokenChangeKeepsLastState covers the second half of CAT-003.
func TestBrokenChangeKeepsLastState(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	f.write(t, "ark.yaml", custom("ark", "ARK"))
	_ = f.im.Sync(ctx)
	f.write(t, "ark.yaml", "schema: 1\nid: ark\nname: ARK broken\n") // protocol missing
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	name, _, _, lastErr, n := f.profile(t, "ark")
	if name != "ARK" || n != 1 {
		t.Errorf("last valid state lost: %q, %d commands", name, n)
	}
	if !lastErr.Valid || !strings.Contains(lastErr.String, "protocol.type") {
		t.Errorf("error not kept: %v", lastErr)
	}
	logtest.Wait(t, f.logs, `"event":"profile.rejected"`)
	f.write(t, "ark.yaml", custom("ark", "ARK fixed"))
	_ = f.im.Sync(ctx)
	if name, _, _, lastErr, _ := f.profile(t, "ark"); name != "ARK fixed" || lastErr.Valid {
		t.Errorf("after the fix: %q, error %v", name, lastErr)
	}
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	for !cond() {
		if time.Since(start) > limit {
			t.Fatalf("condition not met within %v", limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return time.Since(start)
}

// TestWatchByEvents covers CAT-003 with file events: a new file is in the
// database within 5 s, without polling.
func TestWatchByEvents(t *testing.T) {
	f := newImportFixture(t)
	f.im.interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = f.im.Sync(ctx)
	go f.im.Watch(ctx)
	time.Sleep(100 * time.Millisecond) // watcher registered
	f.write(t, "rust.yaml", custom("rust", "Rust"))
	d := waitFor(t, 5*time.Second, func() bool { name, _, _, _, _ := f.profile(t, "rust"); return name == "Rust" })
	t.Logf("imported after %v", d)
}

// TestWatchByPolling covers the fallback for file systems without events.
func TestWatchByPolling(t *testing.T) {
	f := newImportFixture(t)
	f.im.noNotify = true
	f.im.interval = 100 * time.Millisecond
	f.write(t, "rust.yaml", custom("rust", "Rust"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = f.im.Sync(ctx)
	go f.im.Watch(ctx)
	time.Sleep(50 * time.Millisecond)
	f.write(t, "rust.yaml", custom("rust", "Rust 2"))
	// Some file systems keep the modification time; a new size counts too.
	waitFor(t, 5*time.Second, func() bool { name, _, _, _, _ := f.profile(t, "rust"); return name == "Rust 2" })
}

// TestImportKeepsProtocolC25 covers D-076: a new version of a profile that
// a server uses may not change its protocol or dialect; the last valid
// state stays and the error is kept. An unused profile may change.
func TestImportKeepsProtocolC25(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	f.write(t, "game.yaml", custom("game", "Game"))
	f.write(t, "free.yaml", custom("free", "Free"))
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'game'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 's1', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', ?, 0)`, id); err != nil {
		t.Fatal(err)
	}
	other := "schema: 1\nid: %s\nname: %s\nprotocol: { type: http-api, default_port: 7777 }\nhttp_api: { dialect: satisfactory }\n"
	f.write(t, "game.yaml", fmt.Sprintf(other, "game", "Game moved"))
	f.write(t, "free.yaml", fmt.Sprintf(other, "free", "Free moved"))
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	var protocol string
	var lastErr sql.NullString
	if err := f.st.DB.QueryRowContext(ctx, `SELECT protocol, last_error FROM game_profiles WHERE slug = 'game'`).Scan(&protocol, &lastErr); err != nil {
		t.Fatal(err)
	}
	if protocol != "source-rcon" || !lastErr.Valid || !strings.Contains(lastErr.String, "server(s) use the profile") {
		t.Errorf("used profile: protocol %q, error %v", protocol, lastErr)
	}
	if err := f.st.DB.QueryRowContext(ctx, `SELECT protocol FROM game_profiles WHERE slug = 'free'`).Scan(&protocol); err != nil || protocol != "http-api" {
		t.Errorf("unused profile: protocol %q %v", protocol, err)
	}
}

// TestImportKeepsProtocolWithParallelProfileChangeC41: a profile list
// change that commits after the import has read the profile and before it
// writes is seen by the protocol check (D-076, SRV-008). The check runs in
// the write transaction of the import, so no server ends with a top
// profile of another protocol or dialect.
func TestImportKeepsProtocolWithParallelProfileChangeC41(t *testing.T) {
	f := newImportFixture(t)
	ctx := context.Background()
	f.write(t, "game.yaml", custom("game", "Game"))
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'game'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `INSERT INTO servers (id, name, protocol, created_at, updated_at) VALUES ('s1', 's1', 'source-rcon', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	// The profile list change in its own write transaction, as
	// servers.Update does: it adds the profile, still source-rcon.
	calls := 0
	f.im.beforeWrite = func() {
		calls++
		tx, err := f.st.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', ?, 0)`, id); err != nil {
			t.Error(err)
			return
		}
		if err := tx.Commit(); err != nil {
			t.Error(err)
		}
	}
	f.write(t, "game.yaml", "schema: 1\nid: game\nname: Game moved\nprotocol: { type: http-api, default_port: 7777 }\nhttp_api: { dialect: satisfactory }\n")
	if err := f.im.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("profile list change ran %d times, want 1", calls)
	}
	var protocol, srvProtocol string
	var lastErr sql.NullString
	if err := f.st.DB.QueryRowContext(ctx, `SELECT protocol, last_error FROM game_profiles WHERE slug = 'game'`).Scan(&protocol, &lastErr); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DB.QueryRowContext(ctx, `SELECT protocol FROM servers WHERE id = 's1'`).Scan(&srvProtocol); err != nil {
		t.Fatal(err)
	}
	if protocol != srvProtocol || !lastErr.Valid || !strings.Contains(lastErr.String, "server(s) use the profile") {
		t.Errorf("server protocol %s, its profile %s, error %v", srvProtocol, protocol, lastErr)
	}
}
