// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { sessionKey } from '@/api/session';
import { commandListsKey, userGroupsKey } from '@/api/users';
import { renderPage, setLanguage } from '@/test/render';
import { OwnedObjects } from './OwnedObjects';

const now = new Date().toISOString();
const session = (role: string) => ({
  user: { id: 'u0', pseudonym: 'boss', role },
  expires_at: now,
  must_change_password: false,
  ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: true },
});
const data = (role: string): [readonly unknown[], unknown][] => [
  [sessionKey, session(role)],
  [
    userGroupsKey,
    [
      {
        id: 'g1',
        name: 'night shift',
        kind: 'moderator',
        owner_id: 'sa',
        members: [],
        grants: [],
        version: 1,
      },
      {
        id: 'g2',
        name: 'other group',
        kind: 'moderator',
        owner_id: 'sa2',
        members: [],
        grants: [],
        version: 1,
      },
    ],
  ],
  [
    commandListsKey,
    [
      { id: 'l1', name: 'basic', owner_id: 'sa', entries: [], version: 1 },
      { id: 'l2', name: 'other list', owner_id: 'sa2', entries: [], version: 1 },
    ],
  ],
];

describe('the objects deleted with a user (D-106)', () => {
  it('names the groups and lists of the users to delete, not those of others', async () => {
    setLanguage('en');
    await renderPage(() => <OwnedObjects ids={['sa']} />, data('rcon_admin'));
    expect(screen.getByText(/deleted with them/)).toBeTruthy();
    expect(screen.getByText('night shift')).toBeTruthy();
    expect(screen.getByText('basic')).toBeTruthy();
    expect(screen.queryByText(/other/)).toBeNull();
  });

  it('shows nothing for users without groups or lists', async () => {
    setLanguage('en');
    const { container } = await renderPage(() => <OwnedObjects ids={['m']} />, data('rcon_admin'));
    expect(container.textContent).toBe('');
  });
});
