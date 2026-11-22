// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package thunk

import "example.invalid/bad/internal/logging"

func Plain(s logging.Secret) string { return logging.Secret.Reveal(s) }
