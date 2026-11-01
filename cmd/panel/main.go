// SPDX-License-Identifier: AGPL-3.0-or-later

// Command panel runs the RCONductor panel: web UI, API and agent port.
package main

import (
	"fmt"
	"os"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
)

func main() {
	fmt.Fprintf(os.Stderr, "rconductor panel %s\n", buildinfo.Version)
}
