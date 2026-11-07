// SPDX-License-Identifier: AGPL-3.0-or-later

// Command panel runs the RCONductor panel: web UI, API and agent port.
// "panel profile check <file>..." checks game profiles without
// configuration, database or network (CAT-006).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NightDragon1/rconductor/internal/app"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/webui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "profile" {
		if len(os.Args) < 3 || os.Args[2] != "check" {
			fmt.Fprintln(os.Stderr, "usage: profile check <file>...")
			os.Exit(2)
		}
		os.Exit(profiles.RunCheck(os.Args[3:], os.Stdout, os.Stderr))
	}
	cfg, err := config.LoadPanelFromOS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rconductor panel: invalid configuration:\n%v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.RunPanel(ctx, cfg, os.Stdout, app.PanelOptions{UI: webui.Handler(cfg.BasePath)}); err != nil {
		stop()
		os.Exit(1) //nolint:gocritic // stop already called
	}
}
