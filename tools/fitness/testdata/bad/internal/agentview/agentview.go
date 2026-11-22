// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentview masks the raw lines it reads: allowed.
package agentview

import (
	"example.invalid/bad/internal/agenthub"
	"example.invalid/bad/internal/cache"
)

func Read(h *agenthub.Hub, d *cache.AgentData) []string {
	_, _ = h.RequestData("a", "logs.tail")
	return d.Logs("a", "c")
}
