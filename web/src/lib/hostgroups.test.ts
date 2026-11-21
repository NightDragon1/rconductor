// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { byAgent, byHost, hostCounts } from './hostgroups';

const now = new Date().toISOString();

function server(id: string, host?: { key: string; display_name?: string }, state?: string): Server {
  return {
    number: 1,
    version: 'v',
    id,
    name: id,
    groups: [],
    status: 'connected',
    status_since: now,
    host_group: host,
    runtime: state
      ? {
          kind: 'runtime',
          server_id: id,
          container: {
            name: id,
            state,
            restart_count: 0,
            restart_loop: false,
            stale: false,
            agent_connected: true,
          },
        }
      : undefined,
  } as Server;
}

describe('host groups (UI-021)', () => {
  it('groups by key, named hosts first by name, servers without agent apart', () => {
    const { hosts, rest } = byHost([
      server('a', { key: '1' }),
      server('b', { key: '2', display_name: 'Zulu' }),
      server('c', { key: '1' }),
      server('d'),
      server('e', { key: '3', display_name: 'Alpha' }),
    ]);
    expect(hosts.map((g) => [g.key, g.displayName, g.servers.map((s) => s.id)])).toEqual([
      ['3', 'Alpha', ['e']],
      ['2', 'Zulu', ['b']],
      ['1', undefined, ['a', 'c']],
    ]);
    expect(rest.map((s) => s.id)).toEqual(['d']);
  });

  it('sums running, stopped and error', () => {
    const s = (id: string, state?: string, status: Server['status'] = 'connected') => ({
      ...server(id, { key: '1' }, state),
      status,
    });
    expect(
      hostCounts([
        s('run', 'running'),
        s('online'),
        s('stop', 'exited'),
        s('gone', 'unknown'),
        s('rcon', 'running', 'offline'),
        s('auth', undefined, 'auth_failed'),
      ] as Server[]),
    ).toEqual({ running: 2, stopped: 1, error: 3 });
  });

  it('counts the health and the named Docker states (UI-021, UI-011)', () => {
    const s = (id: string, state: string, health?: 'healthy' | 'starting' | 'unhealthy') => {
      const v = server(id, { key: '1' }, state);
      const c = v.runtime?.container;
      if (c) c.health = health;
      return v;
    };
    expect(
      hostCounts([
        s('ok', 'running', 'healthy'),
        s('boot', 'running', 'starting'),
        s('sick', 'running', 'unhealthy'),
        s('pause', 'paused'),
        s('again', 'restarting'),
        s('dead', 'dead'),
      ]),
    ).toEqual({ running: 2, stopped: 2, error: 2 });
  });

  it('keeps 500 servers on 50 hosts in order and fast', () => {
    const list = Array.from({ length: 500 }, (_, i) => server(`s${i}`, { key: String(i % 50) }));
    const t0 = performance.now();
    const { hosts } = byHost(list);
    expect(performance.now() - t0).toBeLessThan(100);
    expect(hosts).toHaveLength(50);
    expect(hosts.every((g) => g.servers.length === 10)).toBe(true);
    expect(hosts[0]?.servers.map((s) => s.id).slice(0, 2)).toEqual(['s0', 's50']);
  });
});

describe('hosts of the topology (UI-010, D-089)', () => {
  const on = (id: string, agent?: string, display?: string) =>
    ({
      ...server(id, display ? { key: 'k', display_name: display } : undefined),
      agent_id: agent,
    }) as Server;

  it('groups by agent, named hosts first by name, servers without agent apart', () => {
    const { hosts, rest } = byAgent([
      on('a', 'x'),
      on('b', 'y', 'Zulu'),
      on('c'),
      on('d', 'z', 'Alpha'),
      on('e', 'x'),
    ]);
    expect(hosts.map((h) => [h.agentId, h.displayName, h.servers.map((s) => s.id)])).toEqual([
      ['z', 'Alpha', ['d']],
      ['y', 'Zulu', ['b']],
      ['x', undefined, ['a', 'e']],
    ]);
    expect(rest.map((s) => s.id)).toEqual(['c']);
  });

  it('takes the name of the agent before the host group', () => {
    const { hosts } = byAgent([on('a', 'x', 'From group')], new Map([['x', 'From agent']]));
    expect(hosts[0]?.displayName).toBe('From agent');
  });
});
