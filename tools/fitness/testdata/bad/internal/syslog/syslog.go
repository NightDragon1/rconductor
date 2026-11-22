// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package syslog writes the system log after masking: allowed.
package syslog

import (
	"context"

	"example.invalid/bad/internal/store/sqlitedb"
)

func Write(q *sqlitedb.Queries, text string) error {
	return q.InsertSystemLog(context.Background(), sqlitedb.InsertSystemLogParams{Text: text})
}
