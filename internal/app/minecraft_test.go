// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build minecraft

package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// The Java runtime the pinned server JAR asks for (scripts/minecraft-server.sh).
const javaImage = "eclipse-temurin:25-jre@sha256:ecdc0adb37bde1a281c4d245a3cd535004b1fc8322d560f5b09463b248c81df0"

const serverProperties = `enable-rcon=true
rcon.port=25575
rcon.password=rconpw
online-mode=false
level-type=minecraft\:flat
generate-structures=false
spawn-protection=0
view-distance=2
simulation-distance=2
`

// startMinecraft runs the server JAR named by RCD_MINECRAFT_JAR and returns
// the address of its RCON port.
func startMinecraft(t *testing.T) string {
	t.Helper()
	jar := os.Getenv("RCD_MINECRAFT_JAR")
	if jar == "" {
		t.Fatal("RCD_MINECRAFT_JAR is not set; scripts/minecraft-server.sh DIR fetches the JAR")
	}
	if os.Getenv("MINECRAFT_ACCEPT_EULA") != "true" {
		t.Fatal("the Minecraft EULA (https://aka.ms/MinecraftEULA) is not accepted; set MINECRAFT_ACCEPT_EULA=true")
	}
	ctx := context.Background()
	ctr, err := testcontainers.Run(ctx, javaImage,
		testcontainers.WithExposedPorts("25575/tcp"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{HostFilePath: jar, ContainerFilePath: "/srv/server.jar", FileMode: 0o644},
			testcontainers.ContainerFile{Reader: strings.NewReader(serverProperties), ContainerFilePath: "/srv/server.properties", FileMode: 0o644},
			// Accepted through MINECRAFT_ACCEPT_EULA above.
			testcontainers.ContainerFile{Reader: strings.NewReader("eula=true\n"), ContainerFilePath: "/srv/eula.txt", FileMode: 0o644},
		),
		testcontainers.WithConfigModifier(func(c *container.Config) { c.WorkingDir = "/srv" }),
		testcontainers.WithCmd("java", "-Xmx1G", "-jar", "server.jar", "--nogui"),
		testcontainers.WithWaitStrategyAndDeadline(5*time.Minute, wait.ForLog("RCON running on")),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start minecraft: %v", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "25575/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort(host, port.Port())
}

// direct is a plain RCON client, independent of the panel's adapter: it
// sends one command and collects packets until the server is quiet. Every
// packet goes to the test log with ID, type and length (issue #104).
func direct(t *testing.T, addr, command string) string {
	t.Helper()
	out, err := directErr(addr, command, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// directErr is direct without testing.T, for use in goroutines.
func directErr(addr, command string, logf func(string, ...any)) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	write := func(id, typ int32, body string) error {
		b := make([]byte, 14+len(body))
		binary.LittleEndian.PutUint32(b[0:], uint32(10+len(body))) //nolint:gosec // test commands are short
		binary.LittleEndian.PutUint32(b[4:], uint32(id))           //nolint:gosec // small constants
		binary.LittleEndian.PutUint32(b[8:], uint32(typ))          //nolint:gosec // small constants
		copy(b[12:], body)
		_, err := conn.Write(b)
		return err
	}
	read := func(quiet time.Duration) (int32, string, error) {
		_ = conn.SetReadDeadline(time.Now().Add(quiet))
		var head [12]byte
		if _, err := io.ReadFull(conn, head[:]); err != nil {
			return 0, "", err
		}
		body := make([]byte, binary.LittleEndian.Uint32(head[0:])-8)
		if _, err := io.ReadFull(conn, body); err != nil {
			return 0, "", err
		}
		id := int32(binary.LittleEndian.Uint32(head[4:]))  //nolint:gosec // packet ID
		typ := int32(binary.LittleEndian.Uint32(head[8:])) //nolint:gosec // packet type
		logf("direct %q: packet id=%d type=%d len=%d", command, id, typ, len(body))
		return id, strings.TrimRight(string(body), "\x00"), nil
	}
	if err := write(1, 3, "rconpw"); err != nil {
		return "", err
	}
	if id, _, err := read(5 * time.Second); err != nil || id != 1 {
		return "", fmt.Errorf("direct auth: %d %w", id, err)
	}
	if err := write(2, 2, command); err != nil {
		return "", err
	}
	var out strings.Builder
	for {
		id, body, err := read(2 * time.Second)
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && out.Len() > 0 {
			return out.String(), nil
		}
		if err != nil {
			return "", fmt.Errorf("direct read: %w", err)
		}
		if id == 2 {
			out.WriteString(body)
		}
	}
}

// TestMinecraftConcurrentClients is the diagnosis of issue #104: two RCON
// clients send list at the same moment, twenty times. If Minecraft gives
// a client the output of both, the answer holds the player line twice.
func TestMinecraftConcurrentClients(t *testing.T) {
	addr := startMinecraft(t)
	crossed := 0
	for round := range 20 {
		var wg sync.WaitGroup
		var answers [2]string
		var errs [2]error
		start := make(chan struct{})
		for i := range 2 {
			wg.Go(func() {
				<-start
				answers[i], errs[i] = directErr(addr, "list", func(f string, a ...any) {
					t.Logf("round %d client %d: "+f, append([]any{round, i}, a...)...)
				})
			})
		}
		close(start)
		wg.Wait()
		for i := range 2 {
			if errs[i] != nil {
				t.Fatalf("round %d client %d: %v", round, i, errs[i])
			}
			if strings.Count(answers[i], "players online") > 1 {
				crossed++
				t.Logf("round %d client %d got %q", round, i, answers[i])
			}
		}
	}
	if crossed > 0 {
		t.Errorf("Minecraft mixed the answers of two RCON clients %d times in 20 rounds", crossed)
	}
}

// TestMinecraft is the walking skeleton against a real Minecraft server:
// answers are the same as a direct RCON client gets, also above 4 KB
// (PROT-001), and three users with 20 commands each share one connection
// with every answer at its sender (PROT-002).
func TestMinecraft(t *testing.T) {
	addr := startMinecraft(t)
	s := newStack(t, addr, "rconpw")

	t.Cleanup(func() {
		if t.Failed() {
			for line := range strings.Lines(s.logs.String()) {
				if strings.Contains(line, `"err"`) || strings.Contains(line, "rcon.") {
					t.Log(line)
				}
			}
		}
	})
	ws := s.console("admin")
	r, code := s.send("admin", "list")
	if want := direct(t, addr, "list"); code != http.StatusOK || r.Response != want {
		t.Errorf("list = %d %q, direct %q", code, r.Response, want)
	}
	var ev struct {
		Type string
		Data commandResult
	}
	if err := readJSON(ws, &ev); err != nil || ev.Type != "event" || ev.Data.RequestID != r.RequestID || ev.Data.Response != r.Response {
		t.Errorf("console event = %+v %v", ev, err)
	}
	r, code = s.send("admin", "help")
	if want := direct(t, addr, "help"); code != http.StatusOK || r.Response != want || len(want) <= 4096 {
		t.Errorf("help = %d, %d bytes; direct %d bytes; equal %v", code, len(r.Response), len(want), r.Response == want)
	}

	if _, code := s.send("admin", "scoreboard objectives add rcd dummy"); code != http.StatusOK {
		t.Fatalf("objective = %d", code)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wrong := []string{}
	for _, user := range []string{"admin", "sa1", "sa2"} {
		for i := range 20 {
			wg.Go(func() {
				player := fmt.Sprintf("%s_%d", user, i)
				r, code := s.send(user, fmt.Sprintf("scoreboard players set %s rcd %d", player, i+1))
				if code != http.StatusOK || !strings.Contains(r.Response, player+" ") {
					mu.Lock()
					wrong = append(wrong, fmt.Sprintf("%s: %d %q", player, code, r.Response))
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	if len(wrong) > 0 {
		t.Errorf("answers at the wrong sender or missing: %v", wrong)
	}
	if n := len(s.eventsOf("rcon.connected")); n != 1 {
		t.Errorf("rcon.connected %d times, want 1", n)
	}
}
