// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build race

package logmask

import "time"

// The race detector slows regexp down about tenfold.
const longLineBound = 60 * time.Second
