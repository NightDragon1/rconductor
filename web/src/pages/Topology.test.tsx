// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { agentsKey, serverGroupsKey, serversKey, type Server } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { TopologyPage } from './Topology';

const now = new Date().toISOString();

function session(role: string) {
  return {
    user: { id: 'u0', pseudonym: 'me', role },
    expires_at: now,
    must_change_password: false,
    ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: true },
  };
}

const container = (connected: boolean) => ({
  name: 'mc',
  state: 'running',
  restart_count: 0,
  restart_loop: false,
  stale: false,
  agent_connected: connected,
});

const servers: Server[] = [
  {
    id: 's1',
    name: 'alpha',
    groups: ['g1'],
    status: 'connected',
    status_since: now,
    version: 'v',
    agent_id: 'a1',
    runtime: { kind: 'runtime', server_id: 's1', container: container(true) },
  },
  {
    id: 's2',
    name: 'beta',
    groups: [],
    status: 'connected',
    status_since: now,
    version: 'v',
    agent_id: 'a2',
    runtime: { kind: 'runtime', server_id: 's2', container: container(false) },
  },
  { id: 's3', name: 'gamma', groups: [], status: 'offline', status_since: now, version: 'v' },
];
const groups = [{ id: 'g1', name: 'main', server_ids: ['s1'] }];
const agents = [
  { id: 'a1', name: 'host-1', remote: '10.0.0.5', status: 'active', connected: true },
  { id: 'a2', name: 'host-2', status: 'active', connected: false },
];

function data(role: string) {
  return [
    [sessionKey, session(role)],
    [serversKey, servers],
    [serverGroupsKey, groups],
    [agentsKey, agents],
  ] as [readonly unknown[], unknown][];
}

describe('topology (UI-010)', () => {
  it('shows the RCON admin host, agent name and state by group', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    const main = screen.getByRole('region', { name: 'main' });
    expect(main.textContent).toContain('10.0.0.5');
    expect(main.textContent).toContain('host-1');
    expect(main.textContent).toContain('connected');
    expect(within(main).getByRole('link', { name: 'alpha' })).toBeTruthy();
    const rest = screen.getByRole('region', { name: 'Other servers' });
    expect(rest.textContent).toContain('host-2');
    expect(rest.textContent).toContain('disconnected');
    expect(rest.textContent).toContain('Without agent');
    expect(within(rest).getByRole('link', { name: 'gamma' })).toBeTruthy();
  });

  it('gives the server admin agents without names (F10)', async () => {
    setLanguage('en');
    // The cache would hold no agents for him; even so no name shows.
    await renderPage(TopologyPage, data('server_admin'));
    const page = document.body.textContent ?? '';
    expect(page).not.toContain('host-1');
    expect(page).not.toContain('10.0.0.5');
    expect(screen.getAllByText('Agent').length).toBe(2);
    expect(page).toContain('disconnected');
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container: el } = await renderPage(TopologyPage, data('server_admin'));
    const names = /^(aria-label=)?(main|alpha|beta|gamma|→|·)$/;
    expect(unmarked(el).filter((s) => !names.test(s.trim()))).toEqual([]);
  });
});
