// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build eventlog

package logging

import (
	"log/slog"
	"os"
)

// witness also writes every line as JSON to the file RCD_EVENT_WITNESS, for
// the check that each event code occurs in a test run (NFR-016). It exists
// only in test builds with the tag eventlog.
func witness(h slog.Handler, opts *slog.HandlerOptions) slog.Handler {
	path := os.Getenv("RCD_EVENT_WITNESS")
	if path == "" {
		return h
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // a path the test run names
	if err != nil {
		panic(err)
	}
	return slog.NewMultiHandler(h, slog.NewJSONHandler(f, opts))
}
