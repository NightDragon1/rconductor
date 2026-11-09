// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import {
  agentsKey,
  profilesKey,
  serverGroupsKey,
  serversKey,
  usersKey,
  type Server,
} from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { HomePage } from './Home';

const now = new Date().toISOString();

function session(role: string) {
  return {
    user: { id: 'u0', pseudonym: 'me', role },
    expires_at: now,
    must_change_password: false,
    ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: true },
  };
}

const container = {
  name: 'mc-a',
  image: 'sha256:abc',
  state: 'running',
  restart_count: 0,
  restart_loop: false,
  stale: true,
  agent_connected: true,
  cpu_percent: 40,
  mem_bytes: 2 ** 31,
  mem_limit: 2 ** 32,
  metrics_at: now,
  started_at: now,
};

const full: Server[] = [
  {
    id: 's1',
    name: 'alpha',
    profile_ids: ['p1'],
    groups: ['g1'],
    status: 'connected',
    status_since: now,
    version: 'v',
    runtime: { kind: 'runtime', server_id: 's1', players: 3, container },
  },
  {
    id: 's2',
    name: 'beta',
    profile_ids: ['p1'],
    groups: [],
    status: 'auth_failed',
    status_since: now,
    version: 'v',
    runtime: {
      kind: 'runtime',
      server_id: 's2',
      container: { ...container, name: 'mc-b', image: 'itzg/mc', restart_loop: true, stale: false },
    },
  },
];

// What the API gives a moderator: no agent data, but the game version as a
// server datum (UI-006, AGT-006).
const reduced: Server[] = full.map(
  ({ id, name, profile_ids, groups, status, status_since, version }) => ({
    id,
    version,
    name,
    profile_ids,
    groups,
    status,
    status_since,
    runtime: {
      kind: 'runtime',
      server_id: id,
      players: 2,
      ...(id === 's1' ? { game_version: '§a1.21.4' } : {}),
    },
  }),
);

const profiles = [{ id: 'p1', name: 'Minecraft Java' }];
const groups = [{ id: 'g1', name: 'main', server_ids: ['s1'] }];
const agents = [
  { id: 'a1', name: 'host-1', status: 'active', connected: true },
  { id: 'a2', name: 'host-2', status: 'active', connected: false, last_seen_at: now },
];
const users = [
  { id: 'u1', pseudonym: 'kiwi', role: 'supporter', disabled: true },
  { id: 'u2', pseudonym: 'lime', role: 'moderator', disabled: false },
];

function adminData(role = 'rcon_admin') {
  return [
    [sessionKey, session(role)],
    [serversKey, full],
    [profilesKey, profiles],
    [serverGroupsKey, groups],
    [agentsKey, agents],
    [usersKey, users],
  ] as [readonly unknown[], unknown][];
}

// Data, not texts: names, numbers and the image.
const data = /^(aria-label=)?(alpha|beta|main|host-2|kiwi|itzg\/mc|beta, |[\d\s/]+|, )+$/;

describe('landing pages', () => {
  it('shows the RCON admin the health overview with links (UI-003)', async () => {
    setLanguage('en');
    await renderPage(HomePage, adminData());
    const rcon = screen.getByRole('region', { name: 'RCON errors' });
    expect(within(rcon).getByText('1')).toBeTruthy();
    expect(
      within(screen.getByRole('region', { name: 'Restart loops' })).getByText('beta'),
    ).toBeTruthy();
    expect(within(screen.getByRole('region', { name: 'Agents' })).getByText('1 / 2')).toBeTruthy();
    expect(
      within(screen.getByRole('region', { name: 'Users' })).getByText('active · 1 locked'),
    ).toBeTruthy();
    const notices = screen.getByRole('region', { name: 'Notices' });
    const links = within(notices)
      .getAllByRole('link')
      .map((a) => a.getAttribute('href'));
    expect(links).toEqual(['/servers/s2', '/servers/s2', '/admin/agents#a2', '/admin/users#u1']);
  });

  it('counts RCON offline as an error while the container state is unknown (UI-003)', async () => {
    setLanguage('en');
    const offline = (id: string, name: string, state?: string): Server => ({
      id,
      name,
      groups: [],
      status: 'offline',
      status_since: now,
      version: 'v',
      runtime: state
        ? {
            kind: 'runtime',
            server_id: id,
            container: {
              ...container,
              name,
              state,
              stale: true,
              agent_connected: state !== 'unknown',
            },
          }
        : { kind: 'runtime', server_id: id },
    });
    await renderPage(HomePage, [
      [sessionKey, session('rcon_admin')],
      [
        serversKey,
        [
          offline('n1', 'noagent'),
          offline('n2', 'agentgone', 'unknown'),
          offline('n3', 'halted', 'exited'),
        ],
      ],
      [profilesKey, profiles],
      [serverGroupsKey, []],
      [agentsKey, []],
      [usersKey, []],
    ]);
    const rcon = screen.getByRole('region', { name: 'RCON errors' });
    expect(within(rcon).getByText('2')).toBeTruthy();
    expect(within(rcon).getByText('noagent, agentgone')).toBeTruthy();
    const rows = within(screen.getByRole('region', { name: 'Notices' })).getAllByRole('listitem');
    expect(rows.map((r) => r.querySelector('bdi')?.textContent)).toEqual(['noagent', 'agentgone']);
    expect(rows[0]?.textContent).toContain('RCON not reachable');
    expect(rows[0]?.textContent).not.toContain('container running');
  });

  it('groups the cards and shows agent data to admins (UI-004)', async () => {
    setLanguage('en');
    await renderPage(HomePage, adminData('server_admin'));
    expect(screen.queryByRole('region', { name: 'Notices' })).toBeNull();
    const main = screen.getByRole('region', { name: 'main' });
    const alpha = within(main).getByRole('article', { name: 'alpha' });
    // A bare digest shows the profile instead.
    expect(within(alpha).getByText('Profile: Minecraft Java')).toBeTruthy();
    expect(within(alpha).getAllByRole('meter')).toHaveLength(2);
    expect(within(alpha).getByText(/Values stale/)).toBeTruthy();
    const beta = within(screen.getByRole('region', { name: 'Other servers' })).getByRole(
      'article',
      {
        name: 'beta',
      },
    );
    expect(beta.dataset.status).toBe('restartLoop');
    expect(within(beta).getByText('itzg/mc')).toBeTruthy();
  });

  it('gives moderators reduced cards without agent data (UI-006)', async () => {
    setLanguage('en');
    await renderPage(HomePage, [
      [sessionKey, session('moderator')],
      [serversKey, reduced],
      [profilesKey, profiles],
    ]);
    expect(screen.queryByRole('region', { name: 'Notices' })).toBeNull();
    expect(screen.queryAllByRole('meter')).toHaveLength(0);
    const beta = screen.getByRole('article', { name: 'beta' });
    expect(beta.dataset.status).toBe('authFailed');
    expect(within(beta).getByText('Profile: Minecraft Java')).toBeTruthy();
    expect(within(beta).getByText('2')).toBeTruthy();
    // The game version shows without its color code; without one no field.
    const alpha = screen.getByRole('article', { name: 'alpha' });
    expect(within(alpha).getByText('Version')).toBeTruthy();
    expect(within(alpha).getByText('1.21.4')).toBeTruthy();
    expect(within(beta).queryByText('Version')).toBeNull();
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container: el } = await renderPage(HomePage, adminData());
    expect(unmarked(el).filter((s) => !data.test(s))).toEqual([]);
  });
});
