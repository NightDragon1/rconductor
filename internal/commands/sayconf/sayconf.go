// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sayconf holds the setting of the prefix of messages to the
// players (CMD-020, D-082). It lies in the core (cmd), which builds the
// message from it; the API of the server groups writes it.
package sayconf

// Setting is the prefix of messages to the players, one per server group.
const Setting = "say_prefix"

// ScopeServerGroup is the scope type of a setting per server group.
const ScopeServerGroup = "server_group"

// MaxPrefix bounds the prefix in characters.
const MaxPrefix = 16
