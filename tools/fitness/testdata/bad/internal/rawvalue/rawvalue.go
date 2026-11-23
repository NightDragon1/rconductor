// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawvalue reads raw lines through method values: a finding.
package rawvalue

import (
	"context"

	"example.invalid/bad/internal/agenthub"
	"example.invalid/bad/internal/cache"
)

func Read(h *agenthub.Hub, d *cache.AgentData) (string, []string) {
	f, g := h.RequestData, d.Logs
	s, _ := f(context.Background(), "a", "logs.tail", nil, nil)
	return s, g("a", "c")
}
