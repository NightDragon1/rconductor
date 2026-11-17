// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pointer reads a secret through a pointer of its base type.
package pointer

import "example.invalid/bad/internal/logging"

// Plain converts a secret through a pointer.
func Plain(s logging.Secret) string { return *(*string)(&s) }
