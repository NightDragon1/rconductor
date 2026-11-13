// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rcon stands in for the RCON client.
package rcon

// Conn is a connection.
type Conn struct{}

// Execute sends a command.
func (Conn) Execute(string) {}
