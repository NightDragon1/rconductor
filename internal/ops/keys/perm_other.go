// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !unix

package keys

import "log/slog"

// warnPermissions is a no-op where Unix file modes do not apply.
func warnPermissions(*slog.Logger, string) {}
