// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !e2e

package main

import "context"

// controlEnabled tells whether this build has the control listener.
const controlEnabled = false

// startControl starts nothing without the build tag e2e: there is no
// control listener and its URL stays empty.
func startControl(context.Context, string, string, string, string) (string, func(), error) {
	return "", func() {}, nil
}
