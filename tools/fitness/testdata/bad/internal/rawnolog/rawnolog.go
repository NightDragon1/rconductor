// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawnolog orders only what holds no log content: no finding.
package rawnolog

import (
	"context"

	"example.invalid/bad/internal/agenthub"
)

func Sizes(h *agenthub.Hub) (string, error) {
	return h.RequestData(context.Background(), "a", "volumes.size", nil, nil)
}
