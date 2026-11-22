// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package e2e

import (
	"fmt"
	"log/slog"
	"os"
)

// P is a test program: it may print, also past the handler.
func P() *slog.Logger {
	fmt.Println("x")
	println("x")
	fmt.Fprintln(os.Stdout, "x")
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}
