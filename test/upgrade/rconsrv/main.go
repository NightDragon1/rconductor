// SPDX-License-Identifier: AGPL-3.0-or-later

// Command rconsrv is the stand-in game server of the upgrade test
// (test/upgrade/run.sh): the Source RCON test server of internal/rcon/rcontest
// on port 25575 in a container, with the password from RCON_PASSWORD. It
// answers "list" like Minecraft and echoes every other command.
package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

func main() {
	pw := os.Getenv("RCON_PASSWORD")
	if pw == "" {
		log.Fatal("RCON_PASSWORD is empty")
	}
	s, err := rcontest.StartOn("0.0.0.0:25575", pw, rcontest.Minecraft, func(c string) string {
		if strings.HasPrefix(c, "list") {
			return "There are 2 of a max of 20 players online: alice, bob"
		}
		return "ok: " + c
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Print("rcon test server on port 25575")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	s.Close()
}
