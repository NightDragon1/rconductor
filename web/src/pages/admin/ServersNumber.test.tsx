// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { cleanup, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { agentsKey, profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { ServerNumber } from '@/components/ServerNumber';
import { UndoProvider } from '@/lib/undo';
import { renderPage, setLanguage } from '@/test/render';
import { ServersAdminPage } from './ServersAdmin';

const now = new Date().toISOString();
const server = {
  id: 's7',
  number: 7,
  name: 'mc',
  groups: [],
  status: 'connected',
  status_since: now,
  version: 'v',
  profile_ids: [],
};

const session = (role: string, features: string[]) => ({
  user: { id: 'a', pseudonym: 'boss', role },
  expires_at: now,
  must_change_password: false,
  ui: { shortcuts_enabled: false, supporters_enabled: false, supporters_allowed: false, features },
});

const data = (role: string, features: string[]): [readonly unknown[], unknown][] => [
  [sessionKey, session(role, features)],
  [serversKey, [server]],
  [serverGroupsKey, []],
  [profilesKey, []],
  [agentsKey, []],
];

describe('server number (UI-018)', () => {
  it('is the first column ID in Manage servers and server groups with the flag server_number', async () => {
    setLanguage('en');
    await renderPage(
      () => (
        <UndoProvider>
          <ServersAdminPage />
        </UndoProvider>
      ),
      data('rcon_admin', ['server_number']),
    );
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers[0]).toBe('ID');
    expect(screen.getByRole('cell', { name: '7' })).toBeTruthy();
  });

  it('has no column ID while the flag is off', async () => {
    setLanguage('en');
    await renderPage(
      () => (
        <UndoProvider>
          <ServersAdminPage />
        </UndoProvider>
      ),
      data('rcon_admin', []),
    );
    expect(screen.getAllByRole('columnheader').map((h) => h.textContent)).not.toContain('ID');
  });

  it('shows the badge for every role with the flag, nothing without', async () => {
    setLanguage('en');
    await renderPage(() => <ServerNumber number={7} />, data('supporter', ['server_number']));
    expect(screen.getByText('Server number 7')).toBeTruthy();
    expect(screen.getByText('#7').getAttribute('aria-hidden')).toBe('true');
    cleanup();
    await renderPage(() => <ServerNumber number={7} />, data('supporter', []));
    expect(screen.queryByText('Server number 7')).toBeNull();
  });
});
