// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { commandListsKey, managedUsersKey, userGroupsKey } from '@/api/users';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { CommandListsPage } from './CommandLists';
import { GroupsPage } from './Groups';

const now = new Date().toISOString();
const data: [readonly unknown[], unknown][] = [
  [
    sessionKey,
    {
      user: { id: 'a', pseudonym: 'boss', role: 'rcon_admin' },
      expires_at: now,
      must_change_password: false,
      ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: false },
    },
  ],
  [
    commandListsKey,
    [
      {
        id: 'l1',
        name: 'mc-mod',
        owner_id: 'a',
        entries: [
          { path: 'kick', passable: false },
          { path: 'say', passable: true },
        ],
      },
    ],
  ],
  [
    userGroupsKey,
    [
      {
        id: 'g1',
        name: 'mods',
        kind: 'moderator',
        owner_id: 'a',
        command_list_id: 'l1',
        members: ['u1'],
        grants: [{ server_id: 's1' }],
      },
    ],
  ],
  [
    managedUsersKey,
    [
      {
        id: 'u1',
        pseudonym: 'nyx',
        role: 'moderator',
        contacts: {},
        has_avatar: false,
        disabled: false,
        is_emergency: false,
        supporters_allowed: false,
        created_at: now,
      },
    ],
  ],
  [serversKey, [{ id: 's1', name: 'mc', groups: [], status: 'connected', status_since: now }]],
  [serverGroupsKey, []],
  [profilesKey, []],
];
// Data, not texts: names, paths and counts.
const isData = /^(mc-mod|mods|nyx|mc|kick|say|kick, say|\d+|aria-label=.*)$/;

describe('groups and command lists', () => {
  it('groups page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(GroupsPage, data);
    fireEvent.click(screen.getByRole('button', { name: 'mods' }));
    expect(unmarked(container).filter((s) => !isData.test(s))).toEqual([]);
  });

  it('command lists page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(CommandListsPage, data);
    fireEvent.click(screen.getByRole('button', { name: 'mc-mod' }));
    expect(unmarked(container).filter((s) => !isData.test(s))).toEqual([]);
  });
});
