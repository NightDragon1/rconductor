// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { agentsKey, serversKey, type Server } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { TopologyPage } from './Topology';

const now = new Date().toISOString();

function session(role: string, features: string[] = []) {
  return {
    user: { id: 'u0', pseudonym: 'me', role },
    expires_at: now,
    must_change_password: false,
    ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: true, features },
    panel_version: '1.2.0',
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
    number: 1,
    id: 's1',
    name: 'alpha',
    groups: ['g1'],
    status: 'connected',
    status_since: now,
    version: 'v',
    agent_id: 'a1',
    host_group: { key: 'k1', display_name: 'Rack <i>A</i>' },
    runtime: { kind: 'runtime', server_id: 's1', container: container(true) },
  },
  {
    number: 2,
    id: 's2',
    name: 'beta',
    groups: [],
    status: 'connected',
    status_since: now,
    version: 'v',
    agent_id: 'a2',
    runtime: { kind: 'runtime', server_id: 's2', container: container(false) },
  },
  {
    number: 3,
    id: 's3',
    name: 'gamma',
    groups: [],
    status: 'offline',
    status_since: now,
    version: 'v',
  },
];
const agents = [
  {
    id: 'a1',
    name: 'host-1',
    remote: '10.0.0.5',
    status: 'active',
    version: '1.2.3',
    connected: true,
    display_name: 'Rack <i>A</i>',
    docker_host: { name: '<b>nas</b>', ip: '192.168.1.5' },
  },
  {
    id: 'a2',
    name: 'host-2',
    remote: '10.0.0.9',
    status: 'active',
    version: '2.0.0',
    connected: false,
    docker_host: { name: 'box' },
  },
];

function data(role: string, features: string[] = []) {
  return [
    [sessionKey, session(role, features)],
    [serversKey, servers],
    [agentsKey, agents],
  ] as [readonly unknown[], unknown][];
}

// host finds the node of a host: the link to the agent for the RCON admin,
// the box in the list of hosts otherwise.
const hosts = () => screen.getByRole('list', { name: 'Hosts' });
const hostLink = (name: RegExp) => within(hosts()).getByRole('link', { name });

describe('topology (UI-010)', () => {
  it('shows the RCON admin panel → host → server, the host links its agent', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    expect(document.body.textContent).toContain('Panel');
    expect(document.body.textContent).toContain('1.2.0');
    const one = hostLink(/host-1/);
    expect(one.getAttribute('href')).toBe('/admin/agents#a1');
    // The display name as text (SEC-006, D-089), agent, address, version, state.
    expect(one.textContent).toContain('Rack <i>A</i>');
    expect(one.querySelector('i')).toBeNull();
    expect(one.textContent).toContain('10.0.0.5');
    expect(one.textContent).toContain('1.2.3');
    expect(one.textContent).toContain('connected');
    expect(one.textContent).not.toContain('Major version');
    const branch = one.closest('li') as HTMLElement;
    expect(within(branch).getByRole('link', { name: 'alpha' }).getAttribute('href')).toBe(
      '/servers/s1',
    );
    const two = hostLink(/host-2/);
    expect(two.textContent).toContain('Host 1');
    expect(two.textContent).toContain('disconnected');
    const rest = screen.getByText('Without agent').closest('li') as HTMLElement;
    expect(within(rest).getByRole('link', { name: 'gamma' })).toBeTruthy();
  });

  it('marks an agent whose major version differs from the panel', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    expect(hostLink(/host-2/).textContent).toContain('Major version differs from the panel (1)');
    expect(screen.getAllByText(/Major version differs/)).toHaveLength(1);
  });

  it('gives the server admin the display name, no agent data and no link (F10, D-089)', async () => {
    setLanguage('en');
    // The cache would hold no agents for him; even so no agent data shows.
    await renderPage(TopologyPage, data('server_admin'));
    const page = document.body.textContent ?? '';
    expect(page).not.toContain('host-1');
    expect(page).not.toContain('10.0.0.5');
    expect(page).not.toContain('1.2.3');
    expect(page).not.toContain('Major version');
    expect(page).toContain('Rack <i>A</i>');
    expect(page).toContain('Host 1');
    expect(page).toContain('disconnected');
    expect(document.querySelector('a[href^="/admin/agents"]')).toBeNull();
    expect(
      within(hosts())
        .getAllByRole('link')
        .map((a) => a.textContent),
    ).toEqual(['alpha', 'beta', 'gamma']);
  });

  it('shows the agent address and no Docker host while docker_host is off (AGT-014)', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    const one = hostLink(/host-1/);
    expect(one.textContent).toContain('10.0.0.5');
    expect(one.textContent).not.toContain('192.168.1.5');
  });

  it('shows the Docker host as text in place of the agent address with docker_host (AGT-014)', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin', ['docker_host']));
    const one = hostLink(/host-1/);
    expect(one.textContent).toContain('<b>nas</b> (192.168.1.5)');
    expect(one.querySelector('b')).toBeNull();
    // host-1 has a host address: no connection address (AGT-014).
    expect(one.textContent).not.toContain('10.0.0.5');
    // host-2 has none: the connection address the backend sent, marked.
    const two = hostLink(/host-2/);
    expect(two.textContent).toContain('box');
    expect(two.textContent).toContain('Connected from 10.0.0.9');
  });

  it('gives the server admin no Docker host, also with docker_host (AGT-014)', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('server_admin', ['docker_host']));
    const page = document.body.textContent ?? '';
    expect(page).not.toContain('nas');
    expect(page).not.toContain('192.168.1.5');
    expect(page).not.toContain('10.0.0.9');
    expect(page).not.toContain('Connected from');
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container: el } = await renderPage(TopologyPage, data('rcon_admin', ['docker_host']));
    const names =
      /^(Rack <i>A<\/i>|host-[12]|box|<b>nas<\/b>|\(192\.168\.1\.5\)|10\.0\.0\.9|1\.2\.[03]|2\.0\.0|alpha|beta|gamma)$/;
    expect(unmarked(el).filter((s) => !names.test(s.trim()))).toEqual([]);
  });
});
