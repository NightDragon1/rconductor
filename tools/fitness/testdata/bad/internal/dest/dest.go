// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dest is a listed secret destination: its conversion passes.
package dest

import "example.invalid/bad/internal/logging"

// Plain converts a secret where it may.
func Plain(s logging.Secret) string { return string(s) }
