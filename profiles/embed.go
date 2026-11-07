// SPDX-License-Identifier: AGPL-3.0-or-later

// Package profiles embeds the game profiles shipped with the panel
// (Design 7.1); custom profiles in /data/profiles replace them by id.
package profiles

import (
	"embed"
	"io/fs"
)

//go:embed *.yaml
var files embed.FS

// FS returns the shipped profile files.
func FS() fs.FS { return files }
