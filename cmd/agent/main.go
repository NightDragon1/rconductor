// SPDX-License-Identifier: AGPL-3.0-or-later

// Command agent runs the RCONductor host agent. Until milestone M3 it is a
// stub that only starts, logs and waits for a stop signal.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NightDragon1/rconductor/internal/app"
	"github.com/NightDragon1/rconductor/internal/config"
)

func main() {
	cfg, err := config.LoadAgentFromOS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rconductor agent: invalid configuration:\n%v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.RunAgent(ctx, cfg, os.Stdout); err != nil {
		stop()
		os.Exit(1) //nolint:gocritic // stop already called
	}
}
