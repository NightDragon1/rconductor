// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !race

package logmask

import "time"

// longLineBound is the time a 1 MiB line may take.
const longLineBound = 5 * time.Second
