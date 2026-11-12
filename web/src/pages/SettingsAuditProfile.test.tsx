// SPDX-License-Identifier: AGPL-3.0-or-later
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { api } from '@/api/client';
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
    expect(unmarked(container).filter((s) => !data.test(s) && !/^[{}"]/.test(s))).toEqual([]);
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

// The entries of migration 00018 have no actor and may name a deleted
// server; the page filters, pages and shows them like any entry (O1,
// AUD-001).
describe('audit entries of migration 00018 (O1)', () => {
  afterEach(() => vi.restoreAllMocks());
  const dead = '01a1b140-97ff-7839-a109-32e7e13cc60d';
  const removal = {
    id: 'e-mig',
    ts: now,
    kind: 'admin',
    action: 'assignment.remove',
    server_id: dead,
    target_type: 'user',
    target_id: 'u1',
    payload: {
      section: 'admin_scopes',
      id: 'sc1',
      entry: { server_id: dead },
      cause: 'target_deleted',
    },
    result: 'ok',
  };
  const normal = {
    id: 'e-own',
    ts: now,
    kind: 'admin',
    action: 'server.update',
    actor_id: 'a',
    actor_role: 'rcon_admin',
    server_id: 's1',
    payload: { name: 'Alpha' },
    result: 'ok',
  };

  it('O1: filters, pages and shows an entry without actor like any other', async () => {
    setLanguage('en');
    const asked: Record<string, unknown>[] = [];
    vi.spyOn(api, 'GET').mockImplementation(((
      _: string,
      init: { params: { query: Record<string, unknown> } },
    ) => {
      const q = init.params.query;
      asked.push(q);
      const items = q.server_id === 's1' ? [normal] : q.cursor ? [normal] : [removal];
      const next = !q.server_id && !q.cursor ? 'c1' : undefined;
      return Promise.resolve({
        data: { items, next_cursor: next },
        response: new Response(null, { status: 200 }),
      });
    }) as never);
    await renderPage(AuditPage, [
      [sessionKey, session],
      [serversKey, [{ id: 's1', name: 'Alpha' }]],
      [managedUsersKey, []],
    ]);
    const list = screen.getByRole('list', { name: 'Audit entries' });
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(1));
    const entry = within(list).getAllByRole('listitem')[0] as HTMLElement;
    expect(entry.textContent).toContain('assignment.remove');
    expect(entry.textContent).toContain('Administration');
    expect(entry.textContent).toContain('OK');
    // No actor, no role, and the deleted server is not shown by its ID in the head.
    expect(entry.textContent).not.toContain('boss');
    expect(entry.textContent).not.toContain('RCON admin');
    expect(entry.firstElementChild?.textContent).not.toContain(dead);
    expect(within(entry).getByText('Details').closest('details')?.textContent).toContain(
      'target_deleted',
    );

    // Paging: the next page comes after it.
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(2));
    expect(asked.at(-1)).toMatchObject({ cursor: 'c1' });
    expect(within(list).getAllByRole('listitem')[1]?.textContent).toContain('boss');

    // Kind and server go to the API as the filters of every entry.
    fireEvent.change(screen.getByLabelText('Kind'), { target: { value: 'admin' } });
    await waitFor(() => expect(asked.at(-1)).toMatchObject({ kind: 'admin' }));
    fireEvent.change(screen.getByLabelText('Server'), { target: { value: 's1' } });
    await waitFor(() => expect(asked.at(-1)).toMatchObject({ kind: 'admin', server_id: 's1' }));
    await waitFor(() =>
      expect(
        within(list)
          .getAllByRole('listitem')
          .map((li) => li.textContent?.includes('server.update')),
      ).toEqual([true]),
    );
    // Only live servers are offered; the deleted one is no option.
    expect(
      Array.from((screen.getByLabelText('Server') as HTMLSelectElement).options).map(
        (o) => o.value,
      ),
    ).toEqual(['', 's1']);
  });
});
