// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sysiface writes the system log through an interface: a finding.
package sysiface

import (
	"context"

	"example.invalid/bad/internal/store/sqlitedb"
)

type writer interface {
	InsertSystemLog(ctx context.Context, p sqlitedb.InsertSystemLogParams) error
}

func Write(w writer) error {
	return w.InsertSystemLog(context.Background(), sqlitedb.InsertSystemLogParams{})
}

func Use() error { return Write(&sqlitedb.Queries{}) }
