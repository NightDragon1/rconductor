// SPDX-License-Identifier: AGPL-3.0-or-later

// Package buildinfo holds release information set at build time.
package buildinfo

import (
	"strconv"
	"strings"
)

// Version is the release version, set via
// -ldflags "-X github.com/NightDragon1/rconductor/internal/buildinfo.Version=1.0".
// Versions have two parts, the first is the major version (D-047).
var Version = "0.0.0-dev"

// Major returns the major version number; panel and agent must match on it (D-023).
// It returns -1 if Version does not start with a number.
func Major() int { return MajorOf(Version) }

// MajorOf returns the major version number of v, or -1 if v does not start
// with a number.
func MajorOf(v string) int {
	v = strings.TrimPrefix(v, "v")
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	if err != nil || n < 0 {
		return -1
	}
	return n
}
