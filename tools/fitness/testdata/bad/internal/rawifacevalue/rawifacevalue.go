// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawifacevalue takes RequestData as a method value of a local
// interface: a finding.
package rawifacevalue

import (
	"context"

	"example.invalid/bad/internal/agenthub"
)

type requester interface {
	RequestData(ctx context.Context, agentID, typ string, payload any, collect func(string)) (string, error)
}

func Read(h *agenthub.Hub) (string, error) {
	var r requester = h
	f := r.RequestData
	return f(context.Background(), "a", "logs.tail", nil, nil)
}
