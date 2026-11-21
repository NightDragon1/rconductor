// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Server } from '@/api/servers';
import { cardStatus, summaryOf } from '@/lib/status';

// HOST_GROUPS_FLAG groups the overview by host (UI-021, D-089).
export const HOST_GROUPS_FLAG = 'host_groups';

export type HostCounts = { running: number; stopped: number; error: number };

export type HostGroup = {
  key: string;
  // displayName is foreign data (SEC-006); undefined shows a placeholder.
  displayName?: string;
  servers: Server[];
  counts: HostCounts;
};

// hostCounts sums the card status of the servers into running, stopped
// and error (UI-021) as summaryOf assigns them.
export function hostCounts(servers: Server[]): HostCounts {
  const out = { running: 0, stopped: 0, error: 0 };
  for (const s of servers) {
    out[summaryOf(cardStatus(s))]++;
  }
  return out;
}

// byHost sorts the cards by the host of their agent (UI-021): hosts with a
// display name by name, the others after them in the order of the
// answer; servers without an agent come apart as the rest.
export function byHost(servers: Server[]): { hosts: HostGroup[]; rest: Server[] } {
  const groups = new Map<string, HostGroup>();
  const rest: Server[] = [];
  for (const s of servers) {
    const h = s.host_group;
    if (!h) {
      rest.push(s);
      continue;
    }
    let g = groups.get(h.key);
    if (!g) {
      g = { key: h.key, displayName: h.display_name, servers: [], counts: hostCounts([]) };
      groups.set(h.key, g);
    }
    g.servers.push(s);
  }
  const hosts = [...groups.values()];
  for (const g of hosts) g.counts = hostCounts(g.servers);
  const named = hosts.filter((g) => g.displayName);
  named.sort((a, b) => (a.displayName ?? '').localeCompare(b.displayName ?? ''));
  return { hosts: [...named, ...hosts.filter((g) => !g.displayName)], rest };
}

export type AgentHost = {
  agentId: string;
  // displayName is foreign data (SEC-006); undefined shows a placeholder.
  displayName?: string;
  servers: Server[];
};

// byAgent sorts the servers by their agent for the topology (UI-010): the
// display name of the host comes from the agent, if the role may read it,
// else from the host group of its servers (D-089). Named hosts first by
// name, the others after them in the order of the answer; servers without
// an agent come apart as the rest.
export function byAgent(
  servers: Server[],
  names: Map<string, string | undefined> = new Map(),
): { hosts: AgentHost[]; rest: Server[] } {
  const groups = new Map<string, AgentHost>();
  const rest: Server[] = [];
  for (const s of servers) {
    if (!s.agent_id) {
      rest.push(s);
      continue;
    }
    let g = groups.get(s.agent_id);
    if (!g) {
      g = { agentId: s.agent_id, servers: [] };
      groups.set(s.agent_id, g);
    }
    g.displayName ??= names.get(s.agent_id) ?? s.host_group?.display_name;
    g.servers.push(s);
  }
  const hosts = [...groups.values()];
  const named = hosts.filter((g) => g.displayName);
  named.sort((a, b) => (a.displayName ?? '').localeCompare(b.displayName ?? ''));
  return { hosts: [...named, ...hosts.filter((g) => !g.displayName)], rest };
}
