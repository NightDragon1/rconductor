// SPDX-License-Identifier: AGPL-3.0-or-later

// Package migrations embeds the versioned schema migrations, one set per
// database (D-018, NFR-013).
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed sqlite/*.sql
var sqliteFS embed.FS

// SQLite returns the migrations for SQLite.
func SQLite() fs.FS {
	sub, err := fs.Sub(sqliteFS, "sqlite")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return sub
}
