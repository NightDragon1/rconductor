// SPDX-License-Identifier: AGPL-3.0-or-later

// Command panel runs the RCONductor panel: web UI, API and agent port.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NightDragon1/rconductor/internal/app"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/webui"
)

func main() {
	cfg, err := config.LoadPanelFromOS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rconductor panel: invalid configuration:\n%v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.RunPanel(ctx, cfg, os.Stdout, app.PanelOptions{UI: webui.Handler()}); err != nil {
		stop()
		os.Exit(1) //nolint:gocritic // stop already called
	}
}
