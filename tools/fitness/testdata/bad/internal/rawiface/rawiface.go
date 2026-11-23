// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawiface fetches raw container logs through an interface: a
// finding.
package rawiface

import (
	"context"

	"example.invalid/bad/internal/agenthub"
)

type requester interface {
	RequestData(ctx context.Context, agentID, typ string, payload any, collect func(string)) (string, error)
}

func Tail(r requester) (string, error) {
	return r.RequestData(context.Background(), "a", "logs.tail", nil, nil)
}

func Use() { _, _ = Tail(&agenthub.Hub{}) }
