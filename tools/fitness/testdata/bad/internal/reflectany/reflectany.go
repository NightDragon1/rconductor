// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package reflectany hands reflect an interface value from elsewhere: a
// finding, its content is not known here.
package reflectany

import "reflect"

func Any(v any) string { return reflect.ValueOf(v).String() }
