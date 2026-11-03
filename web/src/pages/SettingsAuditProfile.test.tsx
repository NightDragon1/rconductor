// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { managedUsersKey } from '@/api/users';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { SettingsPage } from './admin/Settings';
import { AuditPage } from './Audit';
import { ProfilePage } from './Profile';

const now = new Date().toISOString();
const session = {
  user: { id: 'a', pseudonym: 'boss', role: 'rcon_admin' },
  expires_at: now,
  must_change_password: false,
  ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: false },
};
const base: [readonly unknown[], unknown][] = [
  [sessionKey, session],
  [serversKey, []],
  [managedUsersKey, []],
];

describe('settings, audit and profile', () => {
  it('settings page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(SettingsPage, [
      ...base,
      [
        ['settings'],
        {
          values: {
            'invite.lifetime': '72h0m0s',
            'supporters.enabled': true,
            'login.max_failures': 5,
          },
          etag: '"1"',
        },
      ],
    ]);
    // Setting keys and values are data.
    const data = /^(invite\.lifetime|supporters\.enabled|login\.max_failures)$/;
    expect(unmarked(container).filter((s) => !data.test(s))).toEqual([]);
  });

  it('audit page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(AuditPage, [
      ...base,
      [
        ['audit', '', ''],
        {
          pages: [
            {
              items: [
                {
                  id: 'e1',
                  ts: now,
                  kind: 'admin',
                  action: 'user.update',
                  actor_id: 'a',
                  actor_role: 'rcon_admin',
                  payload: { role: 'supporter' },
                  result: 'ok',
                },
              ],
            },
          ],
          pageParams: [undefined],
        },
      ],
    ]);
    // Dates come from the browser's formatting, not the language files.
    const data = /^(user\.update|boss|aria-label=.*|[\d.,:/ APM]+)$/;
    expect(unmarked(container).filter((s) => !data.test(s) && !s.startsWith('{'))).toEqual([]);
  });

  it('profile page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(ProfilePage, [
      ...base,
      [
        ['me'],
        {
          id: 'a',
          pseudonym: 'boss',
          role: 'rcon_admin',
          real_name: 'Bo Ss',
          contacts: { telegram: '@boss' },
          has_avatar: false,
        },
      ],
    ]);
    const data = /^(boss)$/;
    expect(unmarked(container).filter((s) => !data.test(s))).toEqual([]);
  });
});
