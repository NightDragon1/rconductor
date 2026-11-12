// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { agentsKey, profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { addProfile, genericAboveSpecific, moveProfile, topProfile } from '@/lib/profiles';
import { renderPage, setLanguage } from '@/test/render';
import { ServersAdminPage } from './ServersAdmin';

const now = new Date().toISOString();
const hint = 'A generic profile stands above a specific one; the specific one should be on top.';

const profiles = [
  { id: 'mc', name: 'Minecraft', kind: 'generic', protocol: 'source-rcon' },
  { id: 'vanilla', name: 'Vanilla', protocol: 'source-rcon' },
  { id: 'pack', name: 'Modpack', kind: 'specific', protocol: 'source-rcon' },
  { id: 'paper', name: 'Paper', kind: 'specific', protocol: 'source-rcon' },
  { id: 'sf', name: 'Satisfactory', kind: 'generic', protocol: 'http-api' },
] as const;

const data = (ids: string[], role = 'rcon_admin'): [readonly unknown[], unknown][] => [
  [
    sessionKey,
    {
      user: { id: 'a', pseudonym: 'boss', role },
      expires_at: now,
      must_change_password: false,
      ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: false },
    },
  ],
  [
    serversKey,
    [
      {
        id: 's1',
        name: 'mc',
        groups: [],
        status: 'connected',
        status_since: now,
        version: 'v1',
        profile_ids: ids,
        host: 'mc.lan',
        port: 25575,
        credentials: 'ok',
      },
    ],
  ],
  [serverGroupsKey, []],
  [profilesKey, profiles],
  [agentsKey, []],
  [['settings'], { values: { 'agents.fingerprint_pairing': false }, etag: '"e"' }],
];

const panel = () => screen.getByRole('complementary', { name: 'Details of mc' });
const order = () => screen.getAllByTestId('profile-item').map((li) => li.textContent);
const pick = (label: string) => {
  const select = screen.getByLabelText<HTMLSelectElement>('Add a game profile');
  const option = within(select).getByRole<HTMLOptionElement>('option', { name: label });
  fireEvent.change(select, { target: { value: option.value } });
};
const open = async (ids: string[], role?: string) => {
  setLanguage('en');
  await renderPage(ServersAdminPage, data(ids, role));
  fireEvent.click(screen.getByRole('button', { name: 'mc' }));
};

afterEach(() => vi.restoreAllMocks());

describe('profile order helpers (CAT-007)', () => {
  it('inserts a specific profile above the topmost generic one', () => {
    expect(addProfile(['pack', 'mc'], 'paper', [...profiles])).toEqual(['pack', 'paper', 'mc']);
    expect(addProfile(['mc'], 'vanilla', [...profiles])).toEqual(['mc', 'vanilla']);
    expect(addProfile(['pack'], 'paper', [...profiles])).toEqual(['pack', 'paper']);
    expect(addProfile(['mc'], 'mc', [...profiles])).toEqual(['mc']);
  });

  it('moves within bounds only', () => {
    expect(moveProfile(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
    expect(moveProfile(['a', 'b'], 0, 5)).toEqual(['a', 'b']);
  });

  it('finds a generic profile above a specific one', () => {
    expect(genericAboveSpecific(['pack', 'mc'], [...profiles])).toBe(false);
    expect(genericAboveSpecific(['mc', 'pack'], [...profiles])).toBe(true);
    expect(genericAboveSpecific(['vanilla', 'paper'], [...profiles])).toBe(true);
  });

  it('names the top profile with the count of the others', () => {
    const t = (_: string, o: { name: string; count: number }) => `${o.name}, +${o.count}`;
    expect(topProfile(t, ['pack', 'mc', 'paper'], [...profiles])).toBe('Modpack, +2');
    expect(topProfile(t, ['mc'], [...profiles])).toBe('Minecraft');
    expect(topProfile(t, [], [...profiles])).toBe('');
  });
});

describe('ordered profile list of a server (SRV-008, CAT-007)', () => {
  it('adds a specific profile above a generic one and a generic one at the end', async () => {
    await open(['mc']);
    pick('Modpack (specific)');
    expect(order()).toEqual(['Modpack (specific)', 'Minecraft']);
    pick('Vanilla');
    expect(order()).toEqual(['Modpack (specific)', 'Minecraft', 'Vanilla']);
    pick('Paper (specific)');
    expect(order()).toEqual(['Modpack (specific)', 'Paper (specific)', 'Minecraft', 'Vanilla']);
  });

  it('offers only profiles of the protocol of the list', async () => {
    await open(['mc']);
    const select = screen.getByLabelText('Add a game profile');
    const names = within(select)
      .getAllByRole('option')
      .map((o) => o.textContent);
    expect(names).toEqual(['+ Profile', 'Vanilla', 'Modpack (specific)', 'Paper (specific)']);
  });

  it('offers every profile for the first one of a new server', async () => {
    setLanguage('en');
    await renderPage(ServersAdminPage, data([]));
    fireEvent.click(screen.getByRole('button', { name: 'New server' }));
    const select = screen.getByLabelText('Add a game profile');
    expect(within(select).getAllByRole('option')).toHaveLength(profiles.length + 1);
    expect(screen.getByText('No game profile yet.')).toBeTruthy();
  });

  it('removes and moves, and save sends the list in that order', async () => {
    const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({
      data: { id: 's1', name: 'mc', version: 'v2', profile_ids: ['pack', 'mc'] },
      response: new Response(),
    } as never);
    await open(['pack', 'mc', 'vanilla']);
    fireEvent.click(screen.getByRole('button', { name: 'Move Minecraft up' }));
    expect(order()).toEqual(['Minecraft', 'Modpack (specific)', 'Vanilla']);
    expect(screen.getByRole('button', { name: 'Move Minecraft up' })).toHaveProperty(
      'disabled',
      true,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Move Vanilla up' }));
    expect(order()).toEqual(['Minecraft', 'Vanilla', 'Modpack (specific)']);
    fireEvent.click(screen.getByRole('button', { name: 'Move Minecraft down' }));
    expect(order()).toEqual(['Vanilla', 'Minecraft', 'Modpack (specific)']);
    fireEvent.click(screen.getByRole('button', { name: 'Remove Vanilla' }));
    expect(order()).toEqual(['Minecraft', 'Modpack (specific)']);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(patch).toHaveBeenCalled());
    expect(patch.mock.calls[0]?.[1]).toMatchObject({
      params: { header: { 'If-Match': 'v1' } },
      body: { profile_ids: ['mc', 'pack'], name: 'mc' },
    });
    // After save the list is the one the backend returns.
    await waitFor(() => expect(order()).toEqual(['Modpack (specific)', 'Minecraft']));
  });

  it('reorders by drag and drop', async () => {
    await open(['pack', 'mc', 'vanilla']);
    const items = screen.getAllByTestId('profile-item');
    const first = items[0] as HTMLElement;
    const last = items[2] as HTMLElement;
    const dataTransfer = { setData: vi.fn(), effectAllowed: '', dropEffect: '' };
    fireEvent.dragStart(last, { dataTransfer });
    fireEvent.dragOver(first, { dataTransfer });
    fireEvent.drop(first, { dataTransfer });
    expect(order()).toEqual(['Vanilla', 'Modpack (specific)', 'Minecraft']);
  });

  it('marks every draggable entry as a sortable list entry with arrow buttons (UI-001, D-075)', async () => {
    await open(['pack', 'mc']);
    for (const item of screen.getAllByTestId('profile-item')) {
      expect(item.getAttribute('draggable')).toBe('true');
      expect(item.hasAttribute('data-sortable-list')).toBe(true);
      expect(item.parentElement?.tagName).toBe('OL');
      expect(within(item).getAllByRole('button', { name: /^Move .* (up|down)$/ })).toHaveLength(2);
    }
  });

  it('sends the whole list with the connection test', async () => {
    const post = vi.spyOn(api, 'POST').mockResolvedValue({
      data: { result: 'ok' },
      response: new Response(),
    } as never);
    await open(['pack', 'mc']);
    fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post.mock.calls[0]?.[1]).toMatchObject({ body: { profile_ids: ['pack', 'mc'] } });
  });

  it('warns while a generic profile stands above a specific one', async () => {
    await open(['pack', 'mc']);
    expect(screen.queryByText(hint)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Move Minecraft up' }));
    const status = screen.getByText(hint);
    expect(status.getAttribute('role')).toBe('status');
    expect(status.className).toContain('text-warn');
    fireEvent.click(screen.getByRole('button', { name: 'Move Minecraft down' }));
    expect(screen.queryByText(hint)).toBeNull();
  });

  it('shows the top profile with the count of the others in the list', async () => {
    setLanguage('en');
    await renderPage(ServersAdminPage, data(['pack', 'mc', 'vanilla']));
    const row = screen.getByRole('row', { name: /mc\.lan/ });
    expect(within(row).getByText('Modpack, +2')).toBeTruthy();
  });

  it('the http-api parts follow the top profile', async () => {
    await open([]);
    expect(screen.queryByLabelText('New API token')).toBeNull();
    pick('Satisfactory');
    expect(screen.getByLabelText('New API token')).toBeTruthy();
  });
});

describe('server admin and the profile list (SRV-008)', () => {
  it('changes only the profile list; save sends profile_ids alone', async () => {
    const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({
      data: { id: 's1', name: 'mc', version: 'v2', profile_ids: ['pack', 'mc'] },
      response: new Response(),
    } as never);
    await open(['mc'], 'server_admin');
    const p = panel();
    expect(within(p).getByLabelText('Name')).toHaveProperty('disabled', true);
    expect(within(p).getByLabelText('Host')).toHaveProperty('disabled', true);
    expect(within(p).getByLabelText('RCON port')).toHaveProperty('disabled', true);
    expect(within(p).getByLabelText('Agent')).toHaveProperty('disabled', true);
    expect(within(p).queryByRole('button', { name: 'Test connection' })).toBeNull();
    expect(within(p).queryByRole('button', { name: 'Delete server …' })).toBeNull();
    expect(within(p).queryByLabelText('New RCON password')).toBeNull();
    pick('Modpack (specific)');
    fireEvent.click(within(p).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(patch).toHaveBeenCalled());
    expect(patch.mock.calls[0]?.[1]).toEqual({
      params: { path: { id: 's1' }, header: { 'If-Match': 'v1' } },
      body: { profile_ids: ['pack', 'mc'] },
    });
  });
});
