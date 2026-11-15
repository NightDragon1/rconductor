// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command agent runs the RCONductor host agent (Design 1).
// "agent version" and "agent --version" print name, version, commit, build
// date, copyright, license and repository (AGT-016).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/config"
)

func main() {
	if printVersion(os.Args[1:], os.Stdout) {
		return
	}
	cfg, err := config.LoadAgentFromOS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rconductor agent: invalid configuration:\n%v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := agent.Main(ctx, cfg, os.Stdout); err != nil {
		stop()
		os.Exit(1) //nolint:gocritic // stop already called
	}
}

// printVersion prints the banner for "version" and "--version" and reports
// whether it did.
func printVersion(args []string, w io.Writer) bool {
	if len(args) != 1 || (args[0] != "version" && args[0] != "--version") {
		return false
	}
	for _, line := range buildinfo.Banner("Agent") {
		_, _ = fmt.Fprintln(w, line)
	}
	return true
}
