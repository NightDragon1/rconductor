// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"testing"

	"github.com/NightDragon1/rconductor/internal/commands/limits"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC284StoredBoundsOutOfRange (CMD-008, CMD-010): a stored value past
// the upper end of the core bounds, written around the settings check,
// falls back to the default in the pipeline.
func TestC284StoredBoundsOutOfRange(t *testing.T) {
	w := newWorld(t)
	w.setting(users.SettingTemplatesPerServer, limits.MaxTemplatesPerServer+1)
	w.setting(users.SettingMultiParallel, limits.MaxMultiParallel+1)
	w.setting(users.SettingMultiMaxServers, limits.MaxMultiMaxServers+1)
	ctx := context.Background()
	if n := w.svc.templatesPerServer(ctx); n != limits.DefaultTemplatesPerServer {
		t.Errorf("templates per server = %d", n)
	}
	if n := w.svc.multiSetting(ctx, users.SettingMultiParallel, limits.DefaultMultiParallel, limits.MinMultiParallel, limits.MaxMultiParallel); n != limits.DefaultMultiParallel {
		t.Errorf("parallel = %d", n)
	}
	if n := w.svc.multiSetting(ctx, users.SettingMultiMaxServers, limits.DefaultMultiMaxServers, limits.MinMultiMaxServers, limits.MaxMultiMaxServers); n != limits.DefaultMultiMaxServers {
		t.Errorf("max servers = %d", n)
	}
}
