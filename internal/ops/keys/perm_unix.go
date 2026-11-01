// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build unix

package keys

import (
	"log/slog"
	"os"
)

// warnPermissions warns if group or others can access the key file.
func warnPermissions(log *slog.Logger, path string) {
	fi, err := os.Stat(path)
	if err == nil && fi.Mode().Perm()&0o077 != 0 {
		log.Warn("master key file is accessible by group or others; use mode 0600",
			slog.String("path", path), slog.String("mode", fi.Mode().Perm().String()))
	}
}
