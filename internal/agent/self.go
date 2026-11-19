// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"os"
	"regexp"
)

// containerMount finds the ID of the own container in the mounts Docker
// sets up for it (hostname, hosts, resolv.conf).
var containerMount = regexp.MustCompile(`/containers/([0-9a-f]{64})/`)

// ownContainerID reads the ID of the container the agent runs in from
// mountinfo; empty outside a container (D-091).
func ownContainerID(mountinfo string) string {
	b, err := os.ReadFile(mountinfo) //nolint:gosec // a fixed path under /proc, or a test file
	if err != nil {
		return ""
	}
	if m := containerMount.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}
