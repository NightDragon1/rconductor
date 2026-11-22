// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package limits holds the bounds the command pipeline reads its settings
// against (CMD-007, CMD-015). It lies in the core (cmd), which enforces
// them; the settings take their defaults and lower ends from here.
package limits

// Templates per server: default and lowest allowed setting.
const (
	DefaultTemplatesPerServer = 100
	MinTemplatesPerServer     = 1
)

// Entries of the command history per page: default and upper end.
const (
	DefaultAuditLimit = 50
	MaxAuditLimit     = 200
)
