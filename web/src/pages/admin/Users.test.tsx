// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { assignmentsKey, invitesKey, managedUsersKey, userGroupsKey } from '@/api/users';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { UsersPage } from './Users';

const now = new Date().toISOString();
const later = new Date(Date.now() + 3600_000).toISOString();
const users = [
  {
    id: 'u1',
    pseudonym: 'nyx',
    role: 'moderator',
    contacts: { telegram: '@nyx' },
    real_name: 'Nyx N',
    has_avatar: false,
    disabled: false,
    is_emergency: false,
    supporters_allowed: true,
    created_at: now,
    last_login_at: now,
  },
  {
    id: 'u2',
    pseudonym: 'kiwi',
    role: 'supporter',
    contacts: {},
    has_avatar: false,
    disabled: true,
    is_emergency: false,
    supporters_allowed: false,
    created_at: now,
  },
];
const assignments = {
  role: 'moderator',
  admin_scopes: [],
  groups: ['g1'],
  grants: [],
  supporter_grants: [],
};

describe('users page', () => {
  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(UsersPage, [
      [
        sessionKey,
        {
          user: { id: 'a', pseudonym: 'boss', role: 'rcon_admin' },
          expires_at: now,
          must_change_password: false,
          ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: false },
        },
      ],
      [managedUsersKey, users],
      [
        invitesKey,
        [{ id: 'i1', created_by: 'a', role: 'supporter', created_at: now, expires_at: later }],
      ],
      [
        userGroupsKey,
        [{ id: 'g1', name: 'mods', kind: 'moderator', owner_id: 'a', members: ['u1'], grants: [] }],
      ],
      [serversKey, [{ id: 's1', name: 'mc', groups: [], status: 'connected', status_since: now }]],
      [serverGroupsKey, []],
      [assignmentsKey('u1'), assignments],
      [assignmentsKey('u2'), { ...assignments, role: 'supporter', groups: [] }],
    ]);
    fireEvent.click(screen.getByRole('checkbox', { name: /nyx/ }));
    fireEvent.click(screen.getByRole('button', { name: 'nyx' }));
    // Data, not texts: pseudonyms, initials, names and contacts.
    const data = /^(nyx|kiwi|NY|KI|mods|mc|Nyx N|telegram|@nyx|aria-label=.*|title=.*)$/;
    expect(unmarked(container).filter((s) => !data.test(s))).toEqual([]);
  });
});
