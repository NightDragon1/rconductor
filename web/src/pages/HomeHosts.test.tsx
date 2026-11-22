// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { profilesKey, serversKey, type Server } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage } from '@/test/render';
import { HomePage } from './Home';

const now = new Date().toISOString();

function session(features: string[]) {
  return {
    user: { id: 'u0', pseudonym: 'me', role: 'moderator' },
    expires_at: now,
    must_change_password: false,
    ui: { shortcuts_enabled: false, supporters_enabled: true, features },
  };
}

const server = (id: string, status: Server['status'], host?: Server['host_group']): Server =>
  ({
    number: 1,
    version: 'v',
    id,
    name: id,
    groups: [],
    status,
    status_since: now,
    host_group: host,
  }) as Server;

const servers = [
  server('alpha', 'connected', { key: '1', display_name: 'Rack <b>1</b>‮' }),
  server('beta', 'offline', { key: '1', display_name: 'Rack <b>1</b>‮' }),
  server('gamma', 'connected', { key: '2' }),
  server('delta', 'connected'),
];

// The overview by host (UI-021, D-089): a frame per host with the display
// name as text or a neutral placeholder, the count and the status summary;
// servers without agent in their own frame.
describe('overview by host', () => {
  it('frames the cards per host', async () => {
    setLanguage('en');
    await renderPage(HomePage, [
      [sessionKey, session(['host_groups'])],
      [serversKey, servers],
      [profilesKey, []],
    ]);
    const rack = screen.getByRole('region', { name: 'Rack <b>1</b>‮' });
    expect(rack.querySelector('b')).toBeNull();
    expect(rack.textContent).toContain('2 servers · running 1 · stopped 0 · error 1');
    expect(within(rack).getByText('alpha')).toBeTruthy();
    expect(within(rack).getByText('beta')).toBeTruthy();
    const unnamed = screen.getByRole('region', { name: 'Host 1' });
    expect(unnamed.textContent).toContain('1 server · running 1');
    expect(within(unnamed).getByText('gamma')).toBeTruthy();
    const none = screen.getByRole('region', { name: 'Servers without agent' });
    expect(within(none).getByText('delta')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /collapse|expand/i })).toBeNull();
  });
});
