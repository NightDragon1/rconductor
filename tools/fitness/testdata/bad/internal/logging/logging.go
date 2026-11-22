// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logging stands in for the redacting log package.
package logging

import (
	"log/slog"
	"os"
)

// Secret is a secret value.
type Secret string

// Reveal returns the value.
func (s Secret) Reveal() string { return string(s) }

// New is the logger of the package itself: it may build one.
func New() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stdout, nil)) }
