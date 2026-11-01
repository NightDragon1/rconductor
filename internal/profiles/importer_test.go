// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
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
	return "schema: 1\nid: " + id + "\nname: " + name + "\nprotocol: { type: source-rcon, default_port: 27015 }\ncatalog:\n  - path: status\n"
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
	if c := strings.Count(f.logs.String(), `"event":"profile.imported"`); c != 1 {
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
	if !strings.Contains(f.logs.String(), `"event":"profile.overridden"`) {
		t.Error("profile.overridden missing")
	}
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
	if !strings.Contains(f.logs.String(), `"event":"profile.rejected"`) {
		t.Error("profile.rejected missing")
	}
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
