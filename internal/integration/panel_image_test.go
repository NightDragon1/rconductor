// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

// Package integration tests the built images against a real Docker engine.
// Build the images first (make images); RCD_PANEL_IMAGE names the panel
// image, default rconductor-panel:dev.
package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "modernc.org/sqlite"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
)

const nonRootUser = "65532:65532"

func panelImage() string {
	if img := os.Getenv("RCD_PANEL_IMAGE"); img != "" {
		return img
	}
	return "rconductor-panel:dev"
}

// volumes are named per test and removed after the containers.
type volumes struct{ data, secrets, backups string }

func newVolumes(t *testing.T) volumes {
	t.Helper()
	prefix := fmt.Sprintf("rcd-it-%d", time.Now().UnixNano())
	v := volumes{prefix + "-data", prefix + "-secrets", prefix + "-backups"}
	t.Cleanup(func() {
		ctx := context.Background()
		dc, err := testcontainers.NewDockerClientWithOpts(ctx)
		if err != nil {
			t.Errorf("docker client: %v", err)
			return
		}
		defer func() { _ = dc.Close() }()
		for _, name := range []string{v.data, v.secrets, v.backups} {
			if _, err := dc.VolumeRemove(ctx, name, client.VolumeRemoveOptions{Force: true}); err != nil {
				t.Errorf("remove volume %s: %v", name, err)
			}
		}
	})
	return v
}

// startPanel runs the panel image without any environment variable, as in
// NFR-001, and waits for app.ready.
func startPanel(ctx context.Context, t *testing.T, v volumes) *testcontainers.DockerContainer {
	t.Helper()
	ctr, err := testcontainers.Run(ctx, panelImage(),
		testcontainers.WithExposedPorts("8080/tcp", "8443/tcp"),
		testcontainers.WithMounts(
			testcontainers.VolumeMount(v.data, "/data"),
			testcontainers.VolumeMount(v.secrets, "/secrets"),
			testcontainers.VolumeMount(v.backups, "/backups"),
		),
		// app.ready is logged once the web port listens.
		testcontainers.WithWaitStrategyAndDeadline(30*time.Second, wait.ForLog("event=app.ready")),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start panel (built with make images?): %v", err)
	}
	return ctr
}

func logs(ctx context.Context, t *testing.T, ctr *testcontainers.DockerContainer) string {
	t.Helper()
	rc, err := ctr.Logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func copyOut(ctx context.Context, t *testing.T, ctr *testcontainers.DockerContainer, path string) []byte {
	t.Helper()
	rc, err := ctr.CopyFileFromContainer(ctx, path)
	if err != nil {
		t.Fatalf("copy %s: %v", path, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// settingsRows returns all settings rows of a database copy as id=key=value.
func settingsRows(t *testing.T, db []byte) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(path, db, 0o600); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	rows, err := conn.QueryContext(context.Background(), "SELECT id, key, value FROM settings ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id, key, value string
		if err := rows.Scan(&id, &key, &value); err != nil {
			t.Fatal(err)
		}
		out = append(out, id+"="+key+"="+value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// stop ends the panel gracefully so SQLite checkpoints its WAL before the
// files are copied out.
func stop(ctx context.Context, t *testing.T, ctr *testcontainers.DockerContainer) {
	t.Helper()
	timeout := 15 * time.Second
	if err := ctr.Stop(ctx, &timeout); err != nil {
		t.Fatal(err)
	}
}

func TestPanelImage(t *testing.T) {
	ctx := context.Background()
	v := newVolumes(t)

	// First start: empty volumes, no environment (NFR-001, NFR-002).
	first := startPanel(ctx, t, v)

	info, err := first.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.User != nonRootUser {
		t.Errorf("image user = %q, want %s", info.Config.User, nonRootUser)
	}
	for _, e := range info.Config.Env {
		if strings.HasPrefix(e, "RCD_") {
			t.Errorf("image sets %s; the panel must start without configuration", e)
		}
	}

	base, err := first.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	for target, want := range map[string]int{"/": http.StatusOK, "/servers/1": http.StatusOK, "/api/v1/x": http.StatusNotFound} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+target, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", target, resp.StatusCode, want)
		}
	}
	// NFR-001: the agent port 8443 speaks TLS 1.3 with the panel key.
	agentPort, err := first.PortEndpoint(ctx, "8443/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	agentKey, err := keys.LoadOrCreateIdentity(slog.New(slog.DiscardHandler), filepath.Join(t.TempDir(), keys.AgentKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg, err := agentproto.ClientConfig(agentKey, "")
	if err != nil {
		t.Fatal(err)
	}
	d := tls.Dialer{Config: tlsCfg}
	conn, err := d.DialContext(ctx, "tcp", agentPort)
	if err != nil {
		t.Fatalf("agent port: %v", err)
	}
	state := conn.(*tls.Conn).ConnectionState()
	_ = conn.Close()
	panelKey, err := agentproto.PeerKey(state)
	if state.Version != tls.VersionTLS13 || err != nil {
		t.Errorf("agent port: TLS %x, key %v", state.Version, err)
	}

	out := logs(ctx, t, first)
	for _, ev := range []string{"event=app.start", "event=db.migrate.done", "event=key.created", "event=auth.initial_password.issued", "event=app.ready"} {
		if !strings.Contains(out, ev) {
			t.Errorf("first start: %s missing in log:\n%s", ev, out)
		}
	}
	if n := strings.Count(out, "password: "); n != 1 {
		t.Errorf("initial password printed %d times on the first start, want 1", n)
	}
	if !strings.Contains(out, "path=/secrets/panel.key") || !strings.Contains(out, "panel_fingerprint="+keys.Fingerprint(panelKey)) {
		t.Errorf("panel key not created in /secrets or not the key of the agent port:\n%s", out)
	}
	if !strings.Contains(out, "path=/secrets/master.key") {
		t.Errorf("master key not created in /secrets:\n%s", out)
	}

	stop(ctx, t, first)
	db := copyOut(ctx, t, first, "/data/rconductor.db")
	key := bytes.TrimSpace(copyOut(ctx, t, first, "/secrets/master.key"))
	before := settingsRows(t, db)
	if len(before) == 0 {
		t.Fatal("no settings rows after first start")
	}

	// SEC-005: neither the encoded nor the raw key is in the database.
	raw, err := base64.StdEncoding.DecodeString(string(key))
	if err != nil || len(raw) != 32 {
		t.Fatalf("master key is not 32 bytes base64: %v", err)
	}
	if bytes.Contains(db, key) || bytes.Contains(db, raw) {
		t.Error("master key found in the database file")
	}

	// Recreate the container with the same volumes (NFR-010).
	if err := first.Terminate(ctx); err != nil {
		t.Fatal(err)
	}
	second := startPanel(ctx, t, v)
	out = logs(ctx, t, second)
	if !strings.Contains(out, "event=key.loaded") || strings.Contains(out, "event=key.created") {
		t.Errorf("second start must load the existing key:\n%s", out)
	}
	// AUTH-007: the initial password is printed exactly once over two starts.
	if strings.Contains(out, "password: ") || strings.Contains(out, "auth.initial_password.issued") {
		t.Errorf("second start issued the initial password again:\n%s", out)
	}
	stop(ctx, t, second)
	if got := bytes.TrimSpace(copyOut(ctx, t, second, "/secrets/master.key")); !bytes.Equal(got, key) {
		t.Error("master key changed after recreating the container")
	}
	if after := settingsRows(t, copyOut(ctx, t, second, "/data/rconductor.db")); !slices.Equal(before, after) {
		t.Errorf("settings changed after recreating the container:\nbefore %v\nafter  %v", before, after)
	}
}
