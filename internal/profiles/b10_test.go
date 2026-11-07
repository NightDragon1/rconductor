// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/logtest"
)

// aliasBomb nests anchors: every level lists the level below width times.
func aliasBomb(levels, width int) string {
	var b strings.Builder
	b.WriteString("x0: &a0 [\"lol\"]\n")
	for i := 1; i <= levels; i++ {
		refs := make([]string, width)
		for j := range refs {
			refs[j] = fmt.Sprintf("*a%d", i-1)
		}
		fmt.Fprintf(&b, "x%d: &a%d [%s]\n", i, i, strings.Join(refs, ","))
	}
	fmt.Fprintf(&b, "match:\n  images: *a%d\n", levels)
	return b.String()
}

// within fails when parse does not end in time.
func within(t *testing.T, limit time.Duration, data []byte) error {
	t.Helper()
	done := make(chan error, 1)
	start := time.Now()
	go func() { _, err := Parse(data); done <- err }()
	select {
	case err := <-done:
		if d := time.Since(start); d > limit {
			t.Errorf("%d bytes took %v", len(data), d)
		}
		return err
	case <-time.After(60 * time.Second):
		t.Fatalf("%d bytes still parsing after 60s", len(data))
		return nil
	}
}

// TestB10_3AliasBomb: a small profile with nested anchors is refused at
// once, with the line of the first alias inside an anchor; at most 100
// aliases are allowed.
func TestB10_3AliasBomb(t *testing.T) {
	err := within(t, 2*time.Second, []byte(aliasBomb(8, 9)))
	if err == nil || !strings.Contains(err.Error(), "[2:") || !strings.Contains(err.Error(), "must not contain aliases") {
		t.Errorf("alias bomb: %v", err)
	}
	many := "schema: 1\nx: &a [1]\nl: [" + strings.TrimSuffix(strings.Repeat("*a,", 101), ",") + "]\n"
	if err := within(t, time.Second, []byte(many)); err == nil || !strings.Contains(err.Error(), "[3:") || !strings.Contains(err.Error(), "more than 100 aliases") {
		t.Errorf("%d aliases: %v", 101, err)
	}
	flat := "schema: 1\nx: &a [1]\nl: [" + strings.TrimSuffix(strings.Repeat("*a,", 100), ",") + "]\n"
	if err := within(t, time.Second, []byte(flat)); err != nil && strings.Contains(err.Error(), "alias") {
		t.Errorf("%d plain aliases refused: %v", 100, err)
	}
}

// TestB10_4DeepNesting: nesting deeper than 64 is refused on the token
// stream, in flow and block style, before it takes much memory.
func TestB10_4DeepNesting(t *testing.T) {
	n := 20000
	data := []byte("schema: 1\nmatch: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n")
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Parse(data)
	runtime.ReadMemStats(&after)
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 64 {
		t.Errorf("%d bytes of nesting allocated %d MB", len(data), mb)
	}
	if err == nil || !strings.Contains(err.Error(), "[2:") || !strings.Contains(err.Error(), "nesting deeper than 64") {
		t.Errorf("flow nesting: %v", err)
	}
	var block strings.Builder
	block.WriteString("schema: 1\n")
	for i := range 65 {
		fmt.Fprintf(&block, "%sk%d:\n", strings.Repeat(" ", i), i)
	}
	if _, err := Parse([]byte(block.String())); err == nil || !strings.Contains(err.Error(), "nesting deeper than 64") {
		t.Errorf("block nesting: %v", err)
	}
	if _, err := Parse([]byte(design74)); err != nil {
		t.Errorf("the example of Design 7.4: %v", err)
	}
}

// TestB10_5TerminalEscapes: profile check writes control characters of an
// unknown key as \x.., so that ESC and BEL never reach the terminal.
func TestB10_5TerminalEscapes(t *testing.T) {
	name := filepath.Join(t.TempDir(), "evil.yaml")
	if err := os.WriteFile(name, []byte("schema: 1\n\"\\e]0;pwned\\a\\e[2J\": 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	RunCheck([]string{name}, &out, &errOut)
	if bytes.ContainsAny(out.Bytes(), "\x1b\a") {
		t.Errorf("control characters in the output: %q", out.String())
	}
	if !strings.Contains(out.String(), `\x1b]0;pwned\x07`) {
		t.Errorf("escaped key missing: %q", out.String())
	}
}

// TestB10ImportSkipsTheBomb: at panel start a refused file is skipped and
// logged like any invalid profile; the shipped profiles still import.
func TestB10ImportSkipsTheBomb(t *testing.T) {
	f := newImportFixture(t)
	f.write(t, "bomb.yaml", aliasBomb(8, 9))
	start := time.Now()
	if err := f.im.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("sync took %v", d)
	}
	logtest.WaitCount(t, f.logs, `"event":"profile.rejected"`, 1)
	if name, _, _, _, _ := f.profile(t, "minecraft-java"); name == "" {
		t.Error("shipped profile not imported")
	}
}
