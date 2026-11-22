// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package quiet

import (
	"bytes"
	"fmt"
	"log/slog"
)

// Q prints into a buffer and builds a logger that drops everything: no
// way out past the handler.
func Q() (*slog.Logger, string) {
	var b bytes.Buffer
	fmt.Fprintln(&b, "x")
	return slog.New(slog.DiscardHandler), b.String()
}
