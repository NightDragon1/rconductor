// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawvar orders version.detect (log content) through a local
// variable: a finding.
package rawvar

import (
	"context"

	"example.invalid/bad/internal/agenthub"
)

func Version(h *agenthub.Hub) (string, error) {
	typ := "version.detect"
	return h.RequestData(context.Background(), "a", typ, nil, nil)
}
