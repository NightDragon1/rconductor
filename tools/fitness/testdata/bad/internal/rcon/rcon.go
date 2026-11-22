// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rcon stands in for the RCON client.
package rcon

import "example.invalid/bad/internal/logging"

// Dial is a revealer: a method value of Reveal is allowed here.
func Dial(s logging.Secret) string {
	f := s.Reveal
	return f()
}

// Conn is a connection.
type Conn struct{}

// Execute sends a command.
func (Conn) Execute(string) {}
