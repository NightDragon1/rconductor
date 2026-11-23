// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package reflectfunc hands reflect a function that returns a secret: a
// finding.
package reflectfunc

import (
	"reflect"

	"example.invalid/bad/internal/logging"
)

func Call(f func() logging.Secret) string { return reflect.ValueOf(f).Call(nil)[0].String() }
