// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package revealifacethunk calls Reveal as a method expression of an
// interface: a finding.
package revealifacethunk

import "example.invalid/bad/internal/logging"

type revealer interface{ Reveal() string }

func Plain(s logging.Secret) string { return revealer.Reveal(s) }
