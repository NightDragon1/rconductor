// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawiface fetches raw container logs through an interface: a
// finding.
package rawiface

import "example.invalid/bad/internal/agenthub"

type requester interface {
	RequestData(agentID, typ string) (string, error)
}

func Tail(r requester) (string, error) { return r.RequestData("a", "logs.tail") }

func Use() { _, _ = Tail(&agenthub.Hub{}) }
