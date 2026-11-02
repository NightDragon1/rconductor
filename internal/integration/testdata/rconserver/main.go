// SPDX-License-Identifier: AGPL-3.0-or-later

// Command rconserver is a Minecraft-like RCON server for the integration
// test of AGT-009: it answers list, and stop makes it exit like a game
// server does, so that the container's restart policy brings it back.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

func main() {
	start := time.Now()
	srv, err := rcontest.StartOn(":25575", "rconpw", rcontest.Minecraft, func(cmd string) string {
		switch cmd {
		case "stop":
			go func() { time.Sleep(100 * time.Millisecond); os.Exit(0) }()
			return "Stopping the server"
		case "uptime":
			return fmt.Sprintf("started %d", start.UnixNano())
		}
		return "There are 0 of a max of 20 players online: "
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("RCON running on :25575")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	srv.Close()
}
