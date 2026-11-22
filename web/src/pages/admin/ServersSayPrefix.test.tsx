// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { agentsKey, profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { UndoProvider } from '@/lib/undo';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { ServersAdminPage } from './ServersAdmin';

const now = new Date().toISOString();
const server = (id: string, name: string) => ({
  id,
  name,
  groups: [],
  status: 'connected',
  status_since: now,
  version: `v-${id}`,
  profile_ids: [],
});
const servers = [server('s1', 'survival'), server('s2', 'creative')];
// Byte order puts "Zeta" before "alpha" (D-082); a locale order would not.
const groups = [
  { id: 'g1', name: 'alpha', server_ids: ['s1', 's2'], version: 'gv1' },
  { id: 'g2', name: 'Zeta', server_ids: ['s1'], version: 'gv2' },
];

const data = (role: string, features: string[]): [readonly unknown[], unknown][] => [
  [
    sessionKey,
    {
      user: { id: 'a', pseudonym: 'boss', role },
      expires_at: now,
      must_change_password: false,
      ui: {
        shortcuts_enabled: false,
        supporters_enabled: false,
        supporters_allowed: false,
        features,
      },
    },
  ],
  [serversKey, servers],
  [serverGroupsKey, groups],
  [profilesKey, []],
  [agentsKey, []],
];

const ok = (body: unknown) => ({ data: body, response: new Response(null, { status: 200 }) });
const refused = (status: number, reason: string) => ({
  error: { title: reason, status, reason },
  response: new Response(null, { status }),
});

type Prefixes = Record<string, { say_prefix: string; version: string } | number>;

// readPrefixes answers the reads of the prefixes; a number is a refusal.
function readPrefixes(prefixes: Prefixes) {
  return vi.spyOn(api, 'GET').mockImplementation((async (
    path: string,
    init?: { params?: { path?: { id?: string } } },
  ) => {
    if (path !== '/server-groups/{id}/say-prefix') return refused(404, 'not_found');
    const p = prefixes[init?.params?.path?.id ?? ''];
    if (p === undefined) return refused(404, 'not_found');
    return typeof p === 'number' ? refused(p, p === 403 ? 'forbidden' : 'not_found') : ok(p);
  }) as never);
}

const prefixReads = (get: { mock: { calls: unknown[][] } }) =>
  get.mock.calls.filter((c) => c[0] === '/server-groups/{id}/say-prefix');

async function open(name: string, role = 'rcon_admin', features = ['say_sender']) {
  await renderPage(
    () => (
      <UndoProvider>
        <ServersAdminPage />
      </UndoProvider>
    ),
    data(role, features),
  );
  fireEvent.click(screen.getByRole('button', { name }));
  return screen.getByRole('complementary');
}

afterEach(() => vi.restoreAllMocks());

describe('say prefix of a server group (CMD-020, D-082)', () => {
  it('shows the prefix', async () => {
    setLanguage('en');
    const get = readPrefixes({
      g1: { say_prefix: '[A]', version: '1' },
      g2: { say_prefix: '', version: '1' },
    });
    const panel = await open('alpha');
    const field = (await within(panel).findByLabelText('Message prefix')) as HTMLInputElement;
    expect(field.value).toBe('[A]');
    expect(prefixReads(get).some((c) => JSON.stringify(c[1]).includes('"g1"'))).toBe(true);
  });

  it('no field when the backend refuses the read (403, 404)', async () => {
    setLanguage('en');
    const get = readPrefixes({ g1: 404, g2: 403 });
    const panel = await open('alpha', 'server_admin');
    await waitFor(() => expect(prefixReads(get).length).toBeGreaterThan(0));
    expect(within(panel).queryByLabelText('Message prefix')).toBeNull();
  });

  it('save sends If-Match with the version read and the new prefix', async () => {
    setLanguage('en');
    readPrefixes({ g1: { say_prefix: '', version: '7' }, g2: { say_prefix: '', version: '1' } });
    const put = vi
      .spyOn(api, 'PUT')
      .mockResolvedValue(ok({ say_prefix: '[R]', version: '8' }) as never);
    const panel = await open('alpha');
    const field = await within(panel).findByLabelText('Message prefix');
    fireEvent.change(field, { target: { value: '[R]' } });
    fireEvent.click(within(panel).getByRole('button', { name: 'Save prefix' }));
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
    expect(put).toHaveBeenCalledWith('/server-groups/{id}/say-prefix', {
      params: { path: { id: 'g1' }, header: { 'If-Match': '7' } },
      body: { say_prefix: '[R]' },
    });
    await waitFor(() =>
      expect(within(panel).getByRole('status').textContent).toBe('Prefix saved.'),
    );
    expect((within(panel).getByLabelText('Message prefix') as HTMLInputElement).value).toBe('[R]');
  });

  it('clear sends an empty prefix', async () => {
    setLanguage('en');
    readPrefixes({ g1: { say_prefix: '[A]', version: '3' }, g2: { say_prefix: '', version: '1' } });
    const put = vi
      .spyOn(api, 'PUT')
      .mockResolvedValue(ok({ say_prefix: '', version: '4' }) as never);
    const panel = await open('alpha');
    fireEvent.click(await within(panel).findByRole('button', { name: 'Clear prefix' }));
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
    expect(put.mock.calls[0]?.[1]).toMatchObject({
      params: { header: { 'If-Match': '3' } },
      body: { say_prefix: '' },
    });
    await waitFor(() =>
      expect(within(panel).getByRole('status').textContent).toBe('Prefix cleared.'),
    );
  });

  it('hints at a prefix that is too long or has §, and names a refusal (400)', async () => {
    setLanguage('en');
    readPrefixes({ g1: { say_prefix: '', version: '1' }, g2: { say_prefix: '', version: '1' } });
    const put = vi.spyOn(api, 'PUT').mockResolvedValue(refused(400, 'invalid') as never);
    const panel = await open('alpha');
    const field = await within(panel).findByLabelText('Message prefix');
    expect(panel.textContent).toContain('At most 16 characters, no §.');
    fireEvent.change(field, { target: { value: 'x'.repeat(17) } });
    expect(panel.textContent).toContain('At most 16 printable characters and no §');
    fireEvent.change(field, { target: { value: '§a[R]' } });
    expect(panel.textContent).toContain('At most 16 printable characters and no §');
    // The hint does not decide: the backend refuses.
    fireEvent.click(within(panel).getByRole('button', { name: 'Save prefix' }));
    await waitFor(() =>
      expect(within(panel).getByRole('alert').textContent).toBe(
        'Not saved: at most 16 printable characters, no §.',
      ),
    );
    expect(put).toHaveBeenCalledTimes(1);
  });

  it('a conflict (409) asks to reload and reads the prefix again', async () => {
    setLanguage('en');
    const prefixes: Prefixes = {
      g1: { say_prefix: '[A]', version: '1' },
      g2: { say_prefix: '', version: '1' },
    };
    const get = readPrefixes(prefixes);
    vi.spyOn(api, 'PUT').mockResolvedValue(refused(409, 'conflict') as never);
    const panel = await open('alpha');
    const field = await within(panel).findByLabelText('Message prefix');
    fireEvent.change(field, { target: { value: '[B]' } });
    fireEvent.click(within(panel).getByRole('button', { name: 'Save prefix' }));
    await waitFor(() =>
      expect(within(panel).getByRole('alert').textContent).toBe('Changed by someone else – reload'),
    );
    expect(within(panel).queryByRole('button', { name: 'Save prefix' })).toBeNull();
    prefixes.g1 = { say_prefix: '[C]', version: '2' };
    const before = prefixReads(get).length;
    fireEvent.click(within(panel).getByRole('button', { name: 'Reload' }));
    await waitFor(() =>
      expect((within(panel).getByLabelText('Message prefix') as HTMLInputElement).value).toBe(
        '[C]',
      ),
    );
    expect(prefixReads(get).length).toBeGreaterThan(before);
    expect(within(panel).getByRole('button', { name: 'Save prefix' })).toBeTruthy();
  });

  it('names a refusal of the write (403, 404) with the fixed texts', async () => {
    setLanguage('en');
    readPrefixes({ g1: { say_prefix: '', version: '1' }, g2: { say_prefix: '', version: '1' } });
    const put = vi
      .spyOn(api, 'PUT')
      .mockResolvedValueOnce(refused(403, 'forbidden') as never)
      .mockResolvedValueOnce(refused(404, 'not_found') as never);
    const panel = await open('alpha');
    const field = await within(panel).findByLabelText('Message prefix');
    fireEvent.change(field, { target: { value: '[R]' } });
    fireEvent.click(within(panel).getByRole('button', { name: 'Save prefix' }));
    await waitFor(() =>
      expect(within(panel).getByRole('alert').textContent).toBe('Not allowed for this user'),
    );
    fireEvent.click(within(panel).getByRole('button', { name: 'Save prefix' }));
    await waitFor(() =>
      expect(within(panel).getByRole('alert').textContent).toBe('Not found or not managed by you'),
    );
    expect(put).toHaveBeenCalledTimes(2);
  });

  it('names which group gives the prefix of a server in several groups', async () => {
    setLanguage('en');
    readPrefixes({
      g1: { say_prefix: '[A]', version: '1' },
      g2: { say_prefix: '[Z]', version: '1' },
    });
    const panel = await open('alpha');
    const hint = await within(panel).findByTestId('say-prefix-several');
    expect(hint.textContent).toContain(
      'Servers in several server groups use the prefix of the first server group by name.',
    );
    // survival is in both: Zeta comes first in byte order.
    await waitFor(() => expect(hint.textContent).toContain('survival uses the prefix of Zeta.'));
    // creative is only in alpha: no line for it.
    expect(hint.textContent).not.toContain('creative');
  });

  it('several groups: a group without a prefix gives way to the next', async () => {
    setLanguage('en');
    readPrefixes({
      g1: { say_prefix: '[A]', version: '1' },
      g2: { say_prefix: '', version: '1' },
    });
    const panel = await open('alpha');
    const hint = await within(panel).findByTestId('say-prefix-several');
    await waitFor(() => expect(hint.textContent).toContain('survival uses the prefix of alpha.'));
  });

  it('several groups: a server admin, who may not see every group of a server, gets only the rule', async () => {
    setLanguage('en');
    const get = readPrefixes({
      g1: { say_prefix: '[A]', version: '1' },
      g2: { say_prefix: '[Z]', version: '1' },
    });
    const panel = await open('alpha', 'server_admin');
    const hint = await within(panel).findByTestId('say-prefix-several');
    expect(hint.textContent).toBe(
      'Servers in several server groups use the prefix of the first server group by name.',
    );
    // Only the prefix of the open group is read, none of the others.
    await waitFor(() => expect(prefixReads(get).length).toBe(1));
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    readPrefixes({
      g1: { say_prefix: '[A]', version: '1' },
      g2: { say_prefix: '[Z]', version: '1' },
    });
    const panel = await open('alpha');
    await within(panel).findByTestId('say-prefix-several');
    await waitFor(() => expect(panel.textContent).toContain('survival'));
    // Names of servers and groups and the prefix itself are data.
    const data = /^(alpha|survival|creative)$/;
    expect(unmarked(panel).filter((s) => !data.test(s))).toEqual([]);
  });
});
