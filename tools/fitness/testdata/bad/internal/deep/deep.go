// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package deep reads a secret through a pointer to a pointer and unsafe;
// without unsafe Go does not convert **Secret to **string.
package deep

import (
	"unsafe"

	"example.invalid/bad/internal/logging"
)

// Plain converts a secret through a pointer to a pointer.
func Plain(s logging.Secret) string {
	p := &s
	return **(**string)(unsafe.Pointer(&p))
}
