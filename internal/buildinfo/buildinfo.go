// SPDX-License-Identifier: AGPL-3.0-or-later

// Package buildinfo holds release information set at build time.
package buildinfo

import (
	"strconv"
	"strings"
)

// Version is the release version, set via
// -ldflags "-X github.com/NightDragon1/rconductor/internal/buildinfo.Version=1.2.3".
var Version = "0.0.0-dev"

// Major returns the major version number; panel and agent must match on it (D-023).
// It returns -1 if Version is not a semantic version.
func Major() int {
	v := strings.TrimPrefix(Version, "v")
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	if err != nil {
		return -1
	}
	return n
}
