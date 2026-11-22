// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package ownlog

import (
	"log/slog"
	"os"
)

func L() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stdout, nil)) }
