// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testConfig(t *testing.T, root string) config.Panel {
	t.Helper()
	cfg, err := config.LoadPanel(func(k string) (string, bool) {
		v, ok := map[string]string{
			"RCD_HTTP_ADDR":   "127.0.0.1:0",
			"RCD_DATA_DIR":    filepath.Join(root, "data"),
			"RCD_SECRETS_DIR": filepath.Join(root, "secrets"),
			"RCD_BACKUP_DIR":  filepath.Join(root, "backups"),
			"RCD_LOG_FORMAT":  "json",
			"RCD_LOG_LEVEL":   "debug",
		}[k]
		return v, ok
	}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// startPanel runs the panel until the returned stop function is called.
func startPanel(t *testing.T, cfg config.Panel, out *syncBuffer) (addr string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	errc := make(chan error, 1)
	go func() {
		errc <- RunPanel(ctx, cfg, out, PanelOptions{Ready: func(a net.Addr) { ready <- a }})
	}()
	select {
	case a := <-ready:
		return a.String(), func() error { cancel(); return <-errc }
	case err := <-errc:
		cancel()
		t.Fatalf("panel did not start: %v\n%s", err, out.String())
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatalf("panel not ready in time:\n%s", out.String())
	}
	return "", nil
}

func events(t *testing.T, logs string) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(strings.NewReader(logs))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not a JSON line: %q", sc.Text())
		}
		for _, k := range []string{logging.KeyTS, logging.KeyLevel, logging.KeyComp} {
			if _, ok := m[k]; !ok {
				t.Errorf("line without %s: %s", k, sc.Text())
			}
		}
		if e, ok := m[logging.KeyEvent].(string); ok {
			out = append(out, e)
		}
	}
	return out
}

func TestPanelStartupSequence(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)

	var first syncBuffer
	addr, stop := startPanel(t, cfg, &first)

	resp, err := http.Get("http://" + addr + "/api/v1/unknown")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("unknown API path: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	got := strings.Join(events(t, first.String()), " ")
	want := "app.start db.migrate.start db.migrate.done key.created app.ready"
	if got != want {
		t.Errorf("first start events = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, store.FileName)); err != nil {
		t.Errorf("database missing: %v", err)
	}

	// Second start with the same directories keeps key and data.
	var second syncBuffer
	_, stop = startPanel(t, cfg, &second)
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got = strings.Join(events(t, second.String()), " ")
	if want := "app.start db.migrate.done key.loaded app.ready"; got != want {
		t.Errorf("second start events = %q, want %q", got, want)
	}
}

func TestPanelFailsOnBadKeyFile(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	cfg.MasterKeyFile = filepath.Join(root, "missing-secret")

	var out syncBuffer
	err := RunPanel(context.Background(), cfg, &out, PanelOptions{})
	if err == nil {
		t.Fatal("panel started without its configured master key")
	}
	if !strings.Contains(out.String(), "panel stopped with error") {
		t.Errorf("error not logged:\n%s", out.String())
	}
}

func TestAgentStub(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- RunAgent(ctx, config.Agent{Log: config.Log{Format: "json"}}, &out) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "app.ready") {
		if time.Now().After(deadline) {
			t.Fatal("agent not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"comp":"agent"`) {
		t.Errorf("agent comp missing:\n%s", out.String())
	}
}
