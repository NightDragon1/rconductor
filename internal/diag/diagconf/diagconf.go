// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package diagconf holds the bounds the diagnostic export reads its
// settings against (NFR-018, D-090). It lies in the core (redact), which
// enforces them; the settings take them from here.
package diagconf

// Size of a package in bytes before compression and exports at the same
// time: default and allowed range.
const (
	DefaultMaxBytes = 32 << 20
	MinMaxBytes     = 1 << 20
	MaxMaxBytes     = 1 << 30
	DefaultParallel = 2
	MinParallel     = 1
	MaxParallel     = 16
)
