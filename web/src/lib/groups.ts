// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Server, ServerGroup } from '@/api/servers';

// byGroups sorts the cards into their server groups (Gesamtkonzept 5);
// a server in two groups shows in both, servers without a group last.
export function byGroups(servers: Server[], groups: ServerGroup[]) {
  const ids = new Set(servers.map((s) => s.id));
  const out = [...groups]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((g) => ({ group: g, servers: servers.filter((s) => g.server_ids.includes(s.id)) }))
    .filter((g) => g.servers.length > 0);
  const grouped = new Set(out.flatMap((g) => g.servers.map((s) => s.id)));
  const rest = servers.filter((s) => ids.has(s.id) && !grouped.has(s.id));
  return { groups: out, rest };
}
