// SPDX-License-Identifier: AGPL-3.0-or-later
import type { components } from '@/api/schema';
import type { Server } from '@/api/servers';

export type Port = components['schemas']['PortRuntime'];

// portsOf gives the ports of a server as the API hands them to the role
// (UI-005): from the agent's snapshot, or for a server without agent its
// configured game ports, not published. The backend decides which kinds a
// role gets; this only shows them.
export function portsOf(s: Server): Port[] {
  const c = s.runtime?.container;
  if (c?.ports) return c.ports;
  return (s.game_ports ?? []).map((g) => ({
    kind: 'game' as const,
    name: g.name,
    private: g.port,
    proto: g.proto,
  }));
}
