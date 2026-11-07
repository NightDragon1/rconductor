// SPDX-License-Identifier: AGPL-3.0-or-later

// Package memlimit keeps the Go heap below the memory limit of the container
// (T-025): without a limit of its own the runtime lets garbage grow to twice
// the live data, and the kernel ends the process.
package memlimit

import (
	"log/slog"
	"math"
	"os"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
)

// share is the part of the container limit the Go runtime aims for. The rest
// is for stacks, the runtime itself and memory outside the heap.
const share = 0.8

// Apply sets the Go memory limit to 80 % of the limit of the cgroup. A value in
// GOMEMLIMIT goes first, the runtime has applied it already; "off" turns the
// limit off.
func Apply(log *slog.Logger) {
	if v := os.Getenv("GOMEMLIMIT"); v != "" {
		log.Info("memory limit from GOMEMLIMIT", slog.String("value", v))
		return
	}
	limit := fromCgroup(os.ReadFile)
	if limit <= 0 {
		log.Info("no memory limit found, the Go runtime runs without one")
		return
	}
	target := int64(float64(limit) * share)
	debug.SetMemoryLimit(target)
	log.Info("memory limit from the cgroup", slog.Int64("limit", limit), slog.Int64("go_limit", target))
}

// fromCgroup returns the lowest memory limit of this process and the cgroups
// above it, or 0 without one. It knows cgroup v2 and v1.
func fromCgroup(read func(string) ([]byte, error)) int64 {
	own, err := read("/proc/self/cgroup")
	if err != nil {
		return 0
	}
	var lowest int64
	consider := func(file string) {
		data, err := read(file)
		if err != nil {
			return
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		// v1 reports no limit as a number near the maximum.
		if err != nil || n <= 0 || n >= math.MaxInt64/2 {
			return
		}
		if lowest == 0 || n < lowest {
			lowest = n
		}
	}
	for line := range strings.Lines(string(own)) {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) != 3 {
			continue
		}
		switch {
		case parts[1] == "":
			for dir := parts[2]; ; dir = path.Dir(dir) {
				consider(path.Join("/sys/fs/cgroup", dir, "memory.max"))
				if dir == "/" || dir == "." {
					break
				}
			}
		case strings.Contains(parts[1], "memory"):
			consider(path.Join("/sys/fs/cgroup/memory", parts[2], "memory.limit_in_bytes"))
			consider("/sys/fs/cgroup/memory/memory.limit_in_bytes")
		}
	}
	return lowest
}
