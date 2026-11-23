// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package reflectchan hands reflect a channel of secrets: a finding.
package reflectchan

import (
	"reflect"

	"example.invalid/bad/internal/logging"
)

func Recv(ch chan logging.Secret) string {
	v, _ := reflect.ValueOf(ch).Recv()
	return v.String()
}
