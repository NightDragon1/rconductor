// SPDX-License-Identifier: AGPL-3.0-or-later

package memlimit

import (
	"bytes"
	"errors"
	"log/slog"
	"math"
	"runtime/debug"
	"strings"
	"testing"
)

func files(m map[string]string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		if v, ok := m[name]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file")
	}
}

func TestFromCgroup(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		want  int64
	}{
		"container, cgroup v2": {map[string]string{"/proc/self/cgroup": "0::/\n", "/sys/fs/cgroup/memory.max": "1073741824\n"}, 1 << 30},
		"no limit":             {map[string]string{"/proc/self/cgroup": "0::/\n", "/sys/fs/cgroup/memory.max": "max\n"}, 0},
		"limit further up": {map[string]string{
			"/proc/self/cgroup": "0::/user.slice/run.scope\n",
			"/sys/fs/cgroup/user.slice/run.scope/memory.max": "max\n",
			"/sys/fs/cgroup/user.slice/memory.max":           "2147483648\n",
		}, 2 << 30},
		"lowest counts": {map[string]string{
			"/proc/self/cgroup":             "0::/a/b\n",
			"/sys/fs/cgroup/a/b/memory.max": "1000\n",
			"/sys/fs/cgroup/a/memory.max":   "5000\n",
		}, 1000},
		"cgroup v1":               {map[string]string{"/proc/self/cgroup": "4:memory:/docker/x\n", "/sys/fs/cgroup/memory/docker/x/memory.limit_in_bytes": "1073741824\n"}, 1 << 30},
		"cgroup v1 without limit": {map[string]string{"/proc/self/cgroup": "4:memory:/\n", "/sys/fs/cgroup/memory/memory.limit_in_bytes": "9223372036854771712\n"}, 0},
		"nothing readable":        {nil, 0},
	} {
		if got := fromCgroup(files(c.files)); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}

// TestGOMEMLIMITGoesFirst: a value of the operator is left alone.
func TestGOMEMLIMITGoesFirst(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "500MiB")
	before := debug.SetMemoryLimit(-1)
	var logs bytes.Buffer
	Apply(slog.New(slog.NewTextHandler(&logs, nil)))
	if got := debug.SetMemoryLimit(-1); got != before {
		t.Errorf("limit changed to %d", got)
	}
	if !strings.Contains(logs.String(), "500MiB") {
		t.Errorf("log: %s", logs.String())
	}
	if before != math.MaxInt64 {
		t.Log("the test process runs with a limit of its own")
	}
}
