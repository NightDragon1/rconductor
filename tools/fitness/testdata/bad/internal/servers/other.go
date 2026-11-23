// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"

	"example.invalid/bad/internal/agenthub"
)

func Raw(h *agenthub.Hub) (string, error) {
	return h.RequestData(context.Background(), "a", "logs.follow", nil, nil)
}
