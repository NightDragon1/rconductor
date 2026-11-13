// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logging stands in for the redacting log package.
package logging

// Secret is a secret value.
type Secret string

// Reveal returns the value.
func (s Secret) Reveal() string { return string(s) }
