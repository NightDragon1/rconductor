// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawparam orders a type it is handed: a finding.
package rawparam

import (
	"context"

	"example.invalid/bad/internal/agenthub"
)

func Order(h *agenthub.Hub, typ string) (string, error) {
	return h.RequestData(context.Background(), "a", typ, nil, nil)
}
