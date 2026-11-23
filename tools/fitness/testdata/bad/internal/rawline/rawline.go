// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rawline names a core file in a //line directive: still a
// finding.
package rawline

import "example.invalid/bad/internal/cache"

//line ../diag/x.go:1
func Read(d *cache.AgentData) []string { return d.Logs("a", "c") }
