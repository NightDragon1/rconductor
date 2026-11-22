// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawlogs reads raw container logs past the masking: a finding.
package rawlogs

import "example.invalid/bad/internal/cache"

func Read(d *cache.AgentData) []string { return d.Logs("a", "c") }
