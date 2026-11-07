// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"context"
	"io"
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// Main runs the host agent until ctx ends: key pair, Docker backend,
// connection to the panel (Design 1). Errors are logged and returned. It
// lives here and not in package app, so that the panel binary never links
// the Docker client (AGT-001).
func Main(ctx context.Context, cfg config.Agent, out io.Writer) error {
	logs := logging.New(out, "agent", cfg.Log)
	log := logs.Root()
	log.Info("starting", logging.Event(logging.AppStart), slog.String(logging.KeyVersion, buildinfo.Version))
	err := runAgent(ctx, cfg, logs)
	if err != nil {
		log.Error("agent stopped with error", slog.Any("err", err))
	}
	return err
}

func runAgent(ctx context.Context, cfg config.Agent, logs *logging.Factory) error {
	log := logs.Root()
	docker, err := NewDocker(cfg, logs.Module("docker"))
	if err != nil {
		return err
	}
	a, err := New(cfg, log, docker)
	if err != nil {
		return err
	}
	log.Info("ready", logging.Event(logging.AppReady), slog.String(logging.KeyVersion, buildinfo.Version),
		slog.String("fingerprint", a.Fingerprint()), slog.String("label", cfg.LabelKey+"="+cfg.LabelValue))
	return a.Run(ctx)
}
