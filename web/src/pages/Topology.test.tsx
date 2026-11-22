// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { agentsKey, serversKey, type Server } from '@/api/servers';
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

function data(role: string) {
  return [
    [sessionKey, session(role)],
    [serversKey, servers],
    [agentsKey, agents],
  ] as [readonly unknown[], unknown][];
}

// The hosts list, its branches (host with its servers) and the node of a
// host: the link to the agent for the RCON admin.
const hosts = () => screen.getByRole('list', { name: 'Hosts' });
const branches = () => [...hosts().children] as [HTMLElement, HTMLElement, HTMLElement];
const agentLink = (id: string) =>
  hosts().querySelector(`a[href="/admin/agents#${id}"]`) as HTMLElement;

describe('topology (UI-010)', () => {
  it('shows the tree panel → hosts → servers, one branch per host', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    expect(document.body.textContent).toContain('Panel');
    expect(document.body.textContent).toContain('1.2.0 · :8080 web · :8443 agents');
    // One branch per host, the servers without agent last; each branch
    // holds its host node and the list of its servers.
    const bs = branches();
    expect(bs.map((b) => b.tagName)).toEqual(['LI', 'LI', 'LI']);
    const serversOf = (b: HTMLElement) =>
      within(b.querySelector('ul') as HTMLElement)
        .getAllByRole('link')
        .map((a) => a.textContent);
    expect(serversOf(bs[0])).toEqual(['alpha']);
    expect(serversOf(bs[1])).toEqual(['beta']);
    expect(serversOf(bs[2])).toEqual(['gamma']);
    expect(bs[0].contains(agentLink('a1'))).toBe(true);
    expect(bs[1].contains(agentLink('a2'))).toBe(true);
    expect(bs[2].textContent).toContain('Without agent');
  });

  it('gives the RCON admin name, agent version and state in the node, linked to the agent', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    const one = agentLink('a1');
    // The display name as text (SEC-006, D-089), then agent version and state.
    expect(one.textContent).toBe('Rack <i>A</i>Agent 1.2.3 · connected');
    expect(one.querySelector('i')).toBeNull();
    expect(within(one).getByText('connected').className).toContain('text-ok');
    const two = agentLink('a2');
    expect(two.textContent).toContain('Host 1');
    expect(two.textContent).toContain('Agent 2.0.0 · disconnected');
    expect(within(two).getByText('disconnected').className).toContain('text-danger');
    // Neither agent ID nor host name nor address in the node.
    for (const [node, name, addr] of [
      [one, 'host-1', '10.0.0.5'],
      [two, 'host-2', '10.0.0.9'],
    ] as const) {
      expect(node.textContent).not.toContain(name);
      expect(node.textContent).not.toContain(addr);
      expect(node.textContent).not.toMatch(/\ba[12]\b/);
    }
  });

  it('shows the servers as cards with the status colour, linked to the server', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    const alpha = within(hosts()).getByRole('link', { name: 'alpha' });
    expect(alpha.getAttribute('href')).toBe('/servers/s1');
    expect(alpha.className).toContain('font-mono');
    const card = alpha.parentElement as HTMLElement;
    expect(card.className).toContain('border-l-ok');
    expect(within(card).getByText('· running').className).toContain('text-ok');
    const gamma = within(hosts()).getByRole('link', { name: 'gamma' }).parentElement as HTMLElement;
    expect(gamma.className).toContain('border-l-danger');
    expect(within(gamma).getByText('· offline').className).toContain('text-danger');
  });

  it('colours each server status by its tone (UI-004)', async () => {
    setLanguage('en');
    const one = (id: string, c: Record<string, unknown>): Server => ({
      ...(servers[0] as Server),
      id,
      name: id,
      runtime: { kind: 'runtime', server_id: id, container: { ...container(true), ...c } },
    });
    const list = [
      one('h', { health: 'healthy' }),
      one('u', { health: 'unhealthy' }),
      one('st', { health: 'starting' }),
      one('r', { state: 'restarting' }),
      one('x', { state: 'exited' }),
    ];
    await renderPage(TopologyPage, [
      [sessionKey, session('rcon_admin')],
      [serversKey, list],
      [agentsKey, agents],
    ]);
    for (const [id, status, tone] of [
      ['h', 'healthy', 'ok'],
      ['u', 'unhealthy', 'danger'],
      ['st', 'starting', 'warn'],
      ['r', 'restarting', 'warn'],
      ['x', 'stopped', 'muted'],
    ]) {
      const card = within(hosts()).getByRole('link', { name: id }).parentElement as HTMLElement;
      expect(within(card).getByText(`· ${status}`).className).toContain(`text-${tone}`);
      expect(card.className).toContain(tone === 'muted' ? 'border-l-border' : `border-l-${tone}`);
    }
  });

  it('marks an agent whose major version differs from the panel', async () => {
    setLanguage('en');
    await renderPage(TopologyPage, data('rcon_admin'));
    expect(agentLink('a2').textContent).toContain('Major version differs from the panel (1)');
    expect(agentLink('a1').textContent).not.toContain('Major version');
    expect(screen.getAllByText(/Major version differs/)).toHaveLength(1);
  });

  it('gives the server admin name and state, no agent version and no link (F10, D-089)', async () => {
    setLanguage('en');
    // The cache would hold no agents for him; even so no agent data shows.
    await renderPage(TopologyPage, data('server_admin'));
    const page = document.body.textContent ?? '';
    expect(page).not.toContain('host-1');
    expect(page).not.toContain('10.0.0.5');
    expect(page).not.toContain('1.2.3');
    expect(page).not.toContain('Major version');
    const bs = branches();
    expect(bs[0].textContent).toContain('Rack <i>A</i>Agent · connected');
    expect(bs[1].textContent).toContain('Host 1Agent · disconnected');
    expect(document.querySelector('a[href^="/admin/agents"]')).toBeNull();
    expect(
      within(hosts())
        .getAllByRole('link')
        .map((a) => a.textContent),
    ).toEqual(['alpha', 'beta', 'gamma']);
  });

  it('keeps Docker host and address out of the node, also with docker_host (AGT-014)', async () => {
    setLanguage('en');
    for (const role of ['rcon_admin', 'server_admin']) {
      const { unmount } = await renderPage(TopologyPage, data(role));
      const page = document.body.textContent ?? '';
      expect(page).not.toContain('nas');
      expect(page).not.toContain('192.168.1.5');
      expect(page).not.toContain('10.0.0.9');
      expect(page).not.toContain('Connected from');
      unmount();
    }
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container: el } = await renderPage(TopologyPage, data('rcon_admin', ['docker_host']));
    const names = /^(Rack <i>A<\/i>|1\.2\.3|2\.0\.0|alpha|beta|gamma)$/;
    expect(unmarked(el).filter((s) => !names.test(s.trim()))).toEqual([]);
  });
});
