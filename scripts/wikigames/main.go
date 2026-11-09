// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// Wikigames prints the wiki page Supported games from the shipped profiles
// (DOC-005, CAT-001); scripts/wiki-supported-games.sh starts it.
package main

import (
	"fmt"
	"os"

	"github.com/NightDragon1/rconductor/internal/profiles"
	shipped "github.com/NightDragon1/rconductor/profiles"
)

func main() {
	page, err := profiles.SupportedGamesPage(shipped.FS())
	if err != nil {
		fmt.Fprintln(os.Stderr, "wikigames:", err)
		os.Exit(1)
	}
	fmt.Print(page)
}
