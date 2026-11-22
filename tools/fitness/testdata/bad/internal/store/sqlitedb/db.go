// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sqlitedb stands for the generated queries.
package sqlitedb

import "context"

type Queries struct{}

type InsertSystemLogParams struct{ Text string }

func (q *Queries) InsertSystemLog(ctx context.Context, p InsertSystemLogParams) error { return nil }
