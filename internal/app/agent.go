// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"io"
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// RunAgent runs the agent stub until ctx ends. Connection to the panel and
// Docker operations follow in M3.
func RunAgent(ctx context.Context, cfg config.Agent, out io.Writer) error {
	log := logging.New(out, "agent", cfg.Log).Root()
	log.Info("starting", logging.Event(logging.AppStart), slog.String(logging.KeyVersion, buildinfo.Version))
	log.Info("ready", logging.Event(logging.AppReady), slog.String(logging.KeyVersion, buildinfo.Version))
	<-ctx.Done()
	return nil
}
