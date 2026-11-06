// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/logtest"
)

// composeDir holds the delivered Compose files, used here unchanged.
const composeDir = "../../deploy/compose/"

// composeImages gives the local images the names of the Compose files.
func composeImages(t *testing.T) {
	t.Helper()
	for local, name := range map[string]string{
		panelImage(): "ghcr.io/nightdragon1/rconductor-panel:1",
		agentImage(): "ghcr.io/nightdragon1/rconductor-agent:1",
	} {
		if out, err := exec.Command("docker", "tag", local, name).CombinedOutput(); err != nil {
			t.Fatalf("docker tag %s: %v: %s", local, err, out)
		}
	}
}

// project is one running Compose project.
type project struct {
	t    *testing.T
	name string
	file string
	env  []string
}

func newProject(t *testing.T, file string, env ...string) *project {
	t.Helper()
	p := &project{t: t, name: uniq("compose"), file: composeDir + file, env: env}
	t.Cleanup(func() {
		if t.Failed() {
			out, _ := p.command("logs", "--no-color").CombinedOutput()
			t.Logf("logs of %s:\n%s", p.name, out)
		}
		if out, err := p.command("down", "--volumes", "--timeout", "2").CombinedOutput(); err != nil {
			t.Errorf("compose down: %v: %s", err, out)
		}
	})
	return p
}

func (p *project) command(args ...string) *exec.Cmd {
	cmd := exec.Command("docker", append([]string{"compose", "-p", p.name, "-f", p.file}, args...)...)
	cmd.Env = append(os.Environ(), p.env...)
	return cmd
}

func (p *project) up(services ...string) {
	p.t.Helper()
	if out, err := p.command(append([]string{"up", "--detach", "--pull", "never"}, services...)...).CombinedOutput(); err != nil {
		p.t.Fatalf("compose up %v: %v: %s", services, err, out)
	}
}

func (p *project) logs(service string) string {
	p.t.Helper()
	out, err := p.command("logs", "--no-color", service).CombinedOutput()
	if err != nil {
		p.t.Fatalf("compose logs: %v: %s", err, out)
	}
	return string(out)
}

// published returns the host address of a container port, empty if it is
// not published. It asks Docker: compose port prints that case differently
// from version to version.
func (p *project) published(service, port string) string {
	p.t.Helper()
	id, err := p.command("ps", "--quiet", service).Output()
	if err != nil {
		p.t.Fatal(err)
	}
	out, err := exec.Command("docker", "port", strings.TrimSpace(string(id)), port+"/tcp").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hardened checks the limits the delivered files set on a container.
func (p *project) hardened(service string) {
	p.t.Helper()
	id, err := p.command("ps", "--quiet", service).Output()
	if err != nil {
		p.t.Fatal(err)
	}
	out, err := exec.Command("docker", "inspect", "--format",
		"{{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}} {{.HostConfig.SecurityOpt}} {{.HostConfig.PidsLimit}} {{.HostConfig.Memory}}",
		strings.TrimSpace(string(id))).Output()
	if err != nil {
		p.t.Fatal(err)
	}
	got := strings.TrimSpace(string(out))
	if want := "true [ALL] [no-new-privileges:true] 256 1073741824"; got != want {
		p.t.Errorf("%s runs with %s, want %s", service, got, want)
	}
}

// noPorts covers AGT-011 for the delivered files: the service publishes no
// port at all.
func (p *project) noPorts(service string) {
	p.t.Helper()
	id, err := p.command("ps", "--quiet", service).Output()
	if err != nil {
		p.t.Fatal(err)
	}
	out, err := exec.Command("docker", "port", strings.TrimSpace(string(id))).Output()
	if err != nil {
		p.t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "" {
		p.t.Errorf("%s publishes %s", service, got)
	}
}

func (p *project) signIn(webPort int) *apiUser {
	p.t.Helper()
	base := "http://localhost:" + strconv.Itoa(webPort)
	waitUntil(p.t, 30*time.Second, "app.ready", func() bool {
		return strings.Contains(p.logs("panel"), "event=app.ready")
	})
	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(p.logs("panel"))
	if m == nil {
		p.t.Fatal("no initial password in the log")
	}
	admin := signIn(p.t, base, "admin", m[1])
	admin.call(http.MethodPost, "/me/password", nil, map[string]string{
		"current_password": m[1], "new_password": "the admin password of this test",
	}, nil, http.StatusNoContent)
	return admin
}

func pairingToken(admin *apiUser) string {
	var tok struct{ Token string }
	admin.call(http.MethodPost, "/agents/tokens", nil, map[string]string{}, &tok, http.StatusCreated)
	return tok.Token
}

func waitConnected(t *testing.T, admin *apiUser) {
	t.Helper()
	waitUntil(t, 30*time.Second, "a connected agent", func() bool {
		var list struct {
			Items []struct{ Connected bool }
		}
		admin.call(http.MethodGet, "/agents", nil, nil, &list, http.StatusOK)
		return len(list.Items) == 1 && list.Items[0].Connected
	})
}

func socketGroup(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("stat", "-L", "-c", "%g", "/var/run/docker.sock").Output()
	if err != nil {
		t.Fatal(err)
	}
	return "DOCKER_GID=" + strings.TrimSpace(string(out))
}

// TestComposeAllInOne covers NFR-014 for Design 5.5: the file starts as it
// is, 8443 stays unpublished, the agent waits without a token and pairs over
// the Compose network with one.
func TestComposeAllInOne(t *testing.T) {
	composeImages(t)
	web := freePort(t)
	env := []string{fmt.Sprintf("RCD_WEB_PORT=%d", web), socketGroup(t)}
	p := newProject(t, "all-in-one.yaml", env...)
	p.up()
	if got := p.published("panel", "8443"); got != "" {
		t.Errorf("port 8443 is published as %s", got)
	}
	if got := p.published("panel", "8080"); !strings.Contains(got, strconv.Itoa(web)) {
		t.Errorf("port 8080 is published as %q, want %d", got, web)
	}
	admin := p.signIn(web)
	waitUntil(t, 15*time.Second, "the agent's hint", func() bool {
		return strings.Contains(p.logs("agent"), "waiting for a pairing token")
	})

	p.env = append(p.env, "RCD_PAIRING_TOKEN="+pairingToken(admin))
	p.up("agent")
	waitConnected(t, admin)
	p.noPorts("agent")
	p.hardened("panel")
	p.hardened("agent")
}

// TestComposeSeparate covers NFR-014 for Design 5.4: panel and agent start
// as two projects, as on two hosts, and pair over the published agent port.
func TestComposeSeparate(t *testing.T) {
	composeImages(t)
	web, agentPort := freePort(t), freePort(t)
	out, err := exec.CommandContext(context.Background(), "docker", "network", "inspect", "bridge",
		"--format", "{{(index .IPAM.Config 0).Gateway}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimSpace(string(out))
	panel := newProject(t, "panel.yaml", fmt.Sprintf("RCD_WEB_PORT=%d", web), fmt.Sprintf("RCD_AGENT_PORT=%d", agentPort),
		fmt.Sprintf("RCD_AGENT_PUBLIC_ADDR=%s:%d", host, agentPort))
	panel.up()
	admin := panel.signIn(web)

	agent := newProject(t, "agent.yaml", socketGroup(t), "RCD_PAIRING_TOKEN="+pairingToken(admin))
	agent.up()
	waitConnected(t, admin)
	agent.noPorts("agent")
}

// TestOtherMajorVersion covers AGT-013 with a real agent of another major
// version against the real panel: refused with reason version, the hint is
// in the log of both, and the agent stays unpaired.
func TestOtherMajorVersion(t *testing.T) {
	ctx := context.Background()
	ctr := startPanel(ctx, t, newVolumes(t))
	base, err := ctr.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	agentPort, err := ctr.PortEndpoint(ctx, "8443/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(logs(ctx, t, ctr))
	if m == nil {
		t.Fatal("no initial password in the log")
	}
	admin := signIn(t, base, "admin", m[1])
	admin.call(http.MethodPost, "/me/password", nil, map[string]string{
		"current_password": m[1], "new_password": "the admin password of this test",
	}, nil, http.StatusNoContent)

	dir := t.TempDir()
	bin := filepath.Join(dir, "agent")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin,
		"-ldflags", "-X github.com/NightDragon1/rconductor/internal/buildinfo.Version=9.0", "../../cmd/agent")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	var out syncBuf
	run, stop := context.WithCancel(ctx)
	cmd := exec.CommandContext(run, bin)
	cmd.Env = []string{
		"RCD_PAIRING_TOKEN=" + pairingToken(admin), "RCD_PANEL_ADDR=" + agentPort, "RCD_SECRETS_DIR=" + dir,
		"RCD_DOCKER_HOST=unix://" + filepath.Join(dir, "no.sock"), "RCD_LOG_FORMAT=json",
	}
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(); _ = cmd.Wait() })

	logtest.Wait(t, &out, `"event":"agent.rejected"`, `"reason":"version"`)
	waitUntil(t, 10*time.Second, "the refusal in the panel log", func() bool {
		l := logs(ctx, t, ctr)
		return strings.Contains(l, "event=agent.rejected") && strings.Contains(l, "reason=version")
	})
	var list struct {
		Items []struct{ Connected bool }
	}
	admin.call(http.MethodGet, "/agents", nil, nil, &list, http.StatusOK)
	if len(list.Items) != 0 {
		t.Errorf("agents after the refusal = %+v", list.Items)
	}
}
