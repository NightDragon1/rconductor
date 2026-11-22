// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package fprint

import (
	"fmt"
	"os"
)

func P() { fmt.Fprintln(os.Stdout, "x") }
