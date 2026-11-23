// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sysvalue writes the system log through a method value and a
// method expression: findings.
package sysvalue

import (
	"context"

	"example.invalid/bad/internal/store/sqlitedb"
)

func Value(q *sqlitedb.Queries) error {
	f := q.InsertSystemLog
	return f(context.Background(), sqlitedb.InsertSystemLogParams{})
}

func Expr(q *sqlitedb.Queries) error {
	return (*sqlitedb.Queries).InsertSystemLog(q, context.Background(), sqlitedb.InsertSystemLogParams{})
}
