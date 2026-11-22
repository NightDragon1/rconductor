// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package limits holds the bounds the command pipeline reads its settings
// against (CMD-007, CMD-008, CMD-010, CMD-015). It lies in the core (cmd), which enforces
// them; the settings take their defaults and lower ends from here.
package limits

// Templates per server (CMD-008, D-068): default and allowed range.
const (
	DefaultTemplatesPerServer = 100
	MinTemplatesPerServer     = 1
	MaxTemplatesPerServer     = 1000
)

// Commands to several servers at once (CMD-010, D-069): servers per call
// and runs at the same time, default and allowed range.
const (
	DefaultMultiMaxServers = 50
	MinMultiMaxServers     = 1
	MaxMultiMaxServers     = 1000
	DefaultMultiParallel   = 8
	MinMultiParallel       = 1
	MaxMultiParallel       = 64
)

// Entries of the command history per page: default and upper end.
const (
	DefaultAuditLimit = 50
	MaxAuditLimit     = 200
)
