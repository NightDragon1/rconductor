// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC16MultiBudget: after the time budget of a multi-server command no
// run starts; the servers left get the result timeout without being sent
// the command, the runs started end normally (CMD-010).
func TestC16MultiBudget(t *testing.T) {
	w := newWorld(t)
	other := w.second(t, "other")
	w.setting(users.SettingMultiParallel, 1)
	w.svc.multiBudget = 50 * time.Millisecond
	_, items, err := w.svc.Multi(context.Background(), w.as("admin"), []string{w.server, other}, "slow 1")
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Result != ResultOK {
		t.Errorf("first = %+v, want ok", items[0])
	}
	if it := items[1]; it.Result != ResultTimeout || it.Reason != ReasonTimeout || it.RequestID != "" {
		t.Errorf("second = %+v, want timeout without a run", it)
	}
	if !strings.Contains(w.logs.String(), `"servers":1`) {
		t.Error("left-out servers not logged")
	}
}
