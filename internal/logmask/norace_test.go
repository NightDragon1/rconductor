// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !race

package logmask

import "time"

// A line of 1 MiB in at most 5 s.
const (
	longLineBytes = 1 << 20
	longLineBound = 5 * time.Second
)
