// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package revealiface takes Reveal as a method value of an interface: a
// finding.
package revealiface

import "example.invalid/bad/internal/logging"

type revealer interface{ Reveal() string }

func Plain(s logging.Secret) string {
	var r revealer = s
	f := r.Reveal
	return f()
}
