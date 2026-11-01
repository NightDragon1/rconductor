// SPDX-License-Identifier: AGPL-3.0-or-later

// Command agent runs the RCONductor host agent. Until milestone M3 it is a
// stub that only starts, logs and waits for a stop signal.
package main

import (
	"fmt"
	"os"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
)

func main() {
	fmt.Fprintf(os.Stderr, "rconductor agent %s\n", buildinfo.Version)
}
