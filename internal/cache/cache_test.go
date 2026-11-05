// SPDX-License-Identifier: AGPL-3.0-or-later

package cache

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

type fakeExec struct {
	calls  atomic.Int64
	answer atomic.Value // string
	fail   atomic.Bool
}

func (f *fakeExec) Execute(_ context.Context, _, cmd string) (string, error) {
	f.calls.Add(1)
	if f.fail.Load() || cmd != "list" {
		return "", errors.New("no connection")
	}
	return f.answer.Load().(string), nil
}

type lockedLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newCache(t *testing.T) (*Cache, *fakeExec, *lockedLog) {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	logs := &lockedLog{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("cache")
	if err := profiles.NewImporter(st, log, os.DirFS("../../profiles"), t.TempDir()).Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO servers (id, name, profile_id, created_at, updated_at)
		SELECT 's1', 'mc', id, 't', 't' FROM game_profiles WHERE slug = 'minecraft-java'`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s2', 'bare', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	ex := &fakeExec{}
	ex.answer.Store("There are 2 of a max of 20 players online: Steve, Alex")
	return New(st, ex, log), ex, logs
}

// TestPlayersCached covers SRV-006: the list comes per command, is kept in
// memory and fetched again only when stale (Overview 4.6).
func TestPlayersCached(t *testing.T) {
	c, ex, _ := newCache(t)
	ctx := context.Background()
	now := time.Now()
	c.now = func() time.Time { return now }
	p, err := c.Players(ctx, "s1")
	if err != nil || !slices.Equal(p.Names, []string{"Steve", "Alex"}) || p.Stale {
		t.Fatalf("players = %+v %v", p, err)
	}
	_, _ = c.Players(ctx, "s1")
	if ex.calls.Load() != 1 {
		t.Errorf("fresh value fetched again: %d calls", ex.calls.Load())
	}
	now = now.Add(PlayersStale + time.Second)
	ex.answer.Store("There are 0 of a max of 20 players online: ")
	if p, _ := c.Players(ctx, "s1"); len(p.Names) != 0 || p.Stale || ex.calls.Load() != 2 {
		t.Errorf("stale value not refreshed: %+v, %d calls", p, ex.calls.Load())
	}
}

// TestStaleKeptAndMarked: a value that cannot be refreshed is shown with
// its time and marked stale, never hidden; a lost connection marks it at
// once and logs cache.stale (NFR-005).
func TestStaleKeptAndMarked(t *testing.T) {
	c, ex, logs := newCache(t)
	ctx := context.Background()
	if _, err := c.Players(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	ex.fail.Store(true)
	c.Invalidate("s1", "rcon_offline")
	p, err := c.Players(ctx, "s1")
	if err != nil || !p.Stale || len(p.Names) != 2 || p.UpdatedAt.IsZero() {
		t.Errorf("after loss: %+v %v", p, err)
	}
	logtest.Wait(t, logs, `"event":"cache.stale"`)
	ex.fail.Store(false)
	if p, _ := c.Players(ctx, "s1"); p.Stale {
		t.Error("still stale after a successful fetch")
	}
	// Nothing cached yet and no connection: an error, not an empty list.
	if _, err := c.Players(ctx, "unknown"); err == nil {
		t.Error("unknown server answered")
	}
	if _, err := c.Players(ctx, "s2"); !errors.Is(err, ErrNoPlayerList) {
		t.Errorf("server without profile: %v", err)
	}
}

// TestRefreshOnlyWhileWatched covers Overview 4.6: the list is fetched at
// once for the first viewer and refreshed regularly only while someone
// views the server.
func TestRefreshOnlyWhileWatched(t *testing.T) {
	c, ex, _ := newCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.refresh = time.Hour
	first := c.Watch(ctx, "s1")
	deadline := time.Now().Add(2 * time.Second)
	for ex.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ex.calls.Load() != 1 {
		t.Fatalf("fetches for the first viewer = %d, want 1 at once", ex.calls.Load())
	}
	first()
	time.Sleep(20 * time.Millisecond)

	c.refresh = 30 * time.Millisecond
	stop := c.Watch(ctx, "s1")
	stop2 := c.Watch(ctx, "s1")
	time.Sleep(200 * time.Millisecond)
	stop()
	if ex.calls.Load() < 3 {
		t.Errorf("refreshes while watched = %d", ex.calls.Load())
	}
	time.Sleep(100 * time.Millisecond)
	stop2()
	time.Sleep(60 * time.Millisecond) // a fetch running at the stop may still end
	after := ex.calls.Load()
	time.Sleep(200 * time.Millisecond)
	if ex.calls.Load() != after {
		t.Error("refresh goes on without viewers")
	}
}

// TestPlayerNamesAreForeignData covers the cache part of SEC-006.
func TestPlayerNamesAreForeignData(t *testing.T) {
	cfg := &profiles.Players{Command: "list", Pattern: "online: (.*)$", Separator: ", "}
	long := strings.Repeat("x", 100)
	names, err := parsePlayers(context.Background(), "online: <script>a</script>, Bob\x1b[31m, "+long+", , \u202eevil", cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"<script>a</script>", "Bob[31m", strings.Repeat("x", MaxPlayerName), "evil"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %q", names)
	}
	many := "online: " + strings.Repeat("p, ", MaxPlayers+50)
	if names, _ := parsePlayers(context.Background(), many, cfg); len(names) != MaxPlayers {
		t.Errorf("count = %d, want %d", len(names), MaxPlayers)
	}
}
