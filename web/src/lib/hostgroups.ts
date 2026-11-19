// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Server } from '@/api/servers';
import { cardStatus } from '@/lib/status';

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
// and error (UI-021); a state that is neither up nor stopped is an error.
export function hostCounts(servers: Server[]): HostCounts {
  const out = { running: 0, stopped: 0, error: 0 };
  for (const s of servers) {
    const st = cardStatus(s);
    if (st === 'running' || st === 'online') out.running++;
    else if (st === 'stopped') out.stopped++;
    else out.error++;
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
