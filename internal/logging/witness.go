// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !eventlog

package logging

import "log/slog"

// witness is a no-op outside the acceptance run, see witness_eventlog.go.
func witness(h slog.Handler, _ *slog.HandlerOptions) slog.Handler { return h }
