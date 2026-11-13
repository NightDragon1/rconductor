// SPDX-License-Identifier: AGPL-3.0-or-later

// Package other sends a command outside the pipeline.
package other

import "example.invalid/bad/internal/rcon"

// X sends.
func X() { rcon.Conn{}.Execute("stop") }
