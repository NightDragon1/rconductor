// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package servers: logs.go masks what it fetches (allowed), other.go of
// the same package does not (a finding), version.go orders no logs.
package servers

import "example.invalid/bad/internal/agenthub"

func Tail(h *agenthub.Hub) (string, error) { return h.RequestData("a", "logs.tail") }
