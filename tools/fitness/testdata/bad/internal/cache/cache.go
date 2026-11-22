// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cache stands for the agent cache with raw log lines.
package cache

type AgentData struct{}

func (d *AgentData) Logs(agentID, container string) []string { return nil }
