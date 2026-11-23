// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package reflectstruct hands reflect a struct with a secret field: a
// finding.
package reflectstruct

import (
	"reflect"

	"example.invalid/bad/internal/logging"
)

type creds struct {
	User string
	Pass logging.Secret
}

func Field(c creds) string { return reflect.ValueOf(c).Field(1).String() }
