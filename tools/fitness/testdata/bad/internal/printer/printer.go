// SPDX-License-Identifier: AGPL-3.0-or-later

// Package printer reveals a secret and prints past the log handler.
package printer

import (
	"fmt"

	"example.invalid/bad/internal/logging"
)

// P prints a secret.
func P() { fmt.Println(logging.Secret("x").Reveal()) }
