// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawthunk reads raw lines through method expressions: a finding.
package rawthunk

import (
	"context"

	"example.invalid/bad/internal/agenthub"
	"example.invalid/bad/internal/cache"
)

func Read(h *agenthub.Hub, d *cache.AgentData) (string, []string) {
	s, _ := (*agenthub.Hub).RequestData(h, context.Background(), "a", "logs.tail", nil, nil)
	return s, (*cache.AgentData).Logs(d, "a", "c")
}
