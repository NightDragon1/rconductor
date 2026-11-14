// SPDX-License-Identifier: AGPL-3.0-or-later
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { agentsKey, profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { UndoProvider } from '@/lib/undo';
import { renderPage, setLanguage } from '@/test/render';
import { ServersAdminPage } from './ServersAdmin';

const now = new Date().toISOString();
const existing = {
  id: 's0',
  name: 'old',
  groups: [],
  status: 'connected',
  status_since: now,
  version: 'v0',
  profile_ids: ['p1'],
  host: 'old.lan',
  port: 25575,
  credentials: 'ok',
};

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
  [serversKey, [existing]],
  [serverGroupsKey, []],
  [profilesKey, [{ id: 'p1', name: 'Minecraft' }]],
  [agentsKey, []],
];

// The answer of the backend: it never holds the password.
const answer = (id: string, name: string) => ({
  data: { ...existing, id, name, host: 'mc.lan', version: `v-${id}`, profile_ids: [] },
  response: new Response(null, { status: 201 }),
});

const page = (role = 'rcon_admin', features = ['server_save_as_new']) =>
  renderPage(
    () => (
      <UndoProvider>
        <ServersAdminPage />
      </UndoProvider>
    ),
    data(role, features),
  );

const panel = () => screen.queryByRole('complementary');

async function createOne() {
  fireEvent.click(screen.getByRole('button', { name: 'New server' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc' } });
  fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'mc.lan' } });
  fireEvent.change(screen.getByLabelText('RCON password'), { target: { value: 'typed-secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create server' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save as New' })).toBeTruthy());
}

const creates = (post: { mock: { calls: unknown[][] } }) =>
  post.mock.calls.filter((c) => c[0] === '/servers');

afterEach(() => vi.restoreAllMocks());

describe('Save as New in the server dialog (UI-017)', () => {
  it('stays open after create and offers Save and Save as New', async () => {
    setLanguage('en');
    const post = vi.spyOn(api, 'POST').mockResolvedValue(answer('s1', 'mc') as never);
    await page();
    await createOne();
    expect(creates(post)).toHaveLength(1);
    expect(panel()).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'mc' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('Server mc created.');
    // The password field starts empty: nothing comes from the answer.
    expect((screen.getByLabelText('New RCON password') as HTMLInputElement).value).toBe('');
  });

  it('Save changes the server just created', async () => {
    setLanguage('en');
    vi.spyOn(api, 'POST').mockResolvedValue(answer('s1', 'mc') as never);
    const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({
      data: { ...answer('s1', 'mc2').data, version: 'v2' },
      response: new Response(),
    } as never);
    await page();
    await createOne();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc2' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1));
    const [path, init] = patch.mock.calls[0] as unknown as [
      string,
      { params: { path: { id: string }; header: { 'If-Match': string } }; body: object },
    ];
    expect(path).toBe('/servers/{id}');
    expect(init.params.path.id).toBe('s1');
    expect(init.params.header['If-Match']).toBe('v-s1');
    expect(init.body).toMatchObject({ name: 'mc2' });
    expect(init.body).not.toHaveProperty('password', expect.anything());
  });

  it('Save as New creates a second server with the password typed again', async () => {
    setLanguage('en');
    const post = vi
      .spyOn(api, 'POST')
      .mockResolvedValueOnce(answer('s1', 'mc') as never)
      .mockResolvedValueOnce(answer('s2', 'mc-b') as never);
    await page();
    await createOne();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc-b' } });
    // Without the password nothing goes out.
    fireEvent.click(screen.getByRole('button', { name: 'Save as New' }));
    expect(screen.getByRole('alert').textContent).toBe(
      'Enter the RCON password again for the new server.',
    );
    expect(creates(post)).toHaveLength(1);
    fireEvent.change(screen.getByLabelText('New RCON password'), {
      target: { value: 'typed-again' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save as New' }));
    await waitFor(() => expect(creates(post)).toHaveLength(2));
    expect(post.mock.calls[0]?.[1]).toMatchObject({
      body: { name: 'mc', host: 'mc.lan', password: 'typed-secret' },
    });
    expect(post.mock.calls[1]?.[1]).toMatchObject({
      body: { name: 'mc-b', host: 'mc.lan', password: 'typed-again' },
    });
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe('Server mc-b created as a new server.'),
    );
    // Save now changes the newest one.
    expect(screen.getByRole('heading', { name: 'mc-b' })).toBeTruthy();
  });

  it('a double click on Save as New sends one request', async () => {
    setLanguage('en');
    let release: (v: unknown) => void = () => undefined;
    const post = vi
      .spyOn(api, 'POST')
      .mockResolvedValueOnce(answer('s1', 'mc') as never)
      .mockImplementationOnce(() => new Promise((r) => (release = r)) as never);
    await page();
    await createOne();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc-b' } });
    fireEvent.change(screen.getByLabelText('New RCON password'), { target: { value: 'x' } });
    const button = screen.getByRole('button', { name: 'Save as New' });
    fireEvent.click(button);
    fireEvent.click(button);
    fireEvent.doubleClick(button);
    await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(true));
    fireEvent.click(button);
    expect(creates(post)).toHaveLength(2);
    await act(async () => release(answer('s2', 'mc-b')));
    await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false));
    expect(creates(post)).toHaveLength(2);
    expect(screen.getByRole('status').textContent).toBe('Server mc-b created as a new server.');
  });

  it('closes without a question when everything is saved', async () => {
    setLanguage('en');
    vi.spyOn(api, 'POST').mockResolvedValue(answer('s1', 'mc') as never);
    await page();
    await createOne();
    // An unsaved change asks first ...
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc-x' } });
    fireEvent.click(screen.getByRole('button', { name: 'Close the details' }));
    expect(screen.getByText('Leave without saving?')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc' } });
    // ... what is saved closes at once.
    fireEvent.click(screen.getByRole('button', { name: 'Close the details' }));
    await waitFor(() => expect(panel()).toBeNull());
    expect(screen.queryByText('Leave without saving?')).toBeNull();
  });

  it('flag off: the panel closes after create, without Save as New', async () => {
    setLanguage('en');
    const post = vi.spyOn(api, 'POST').mockResolvedValue(answer('s1', 'mc') as never);
    await page('rcon_admin', []);
    fireEvent.click(screen.getByRole('button', { name: 'New server' }));
    expect(screen.queryByRole('button', { name: 'Save as New' })).toBeNull();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'mc' } });
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'mc.lan' } });
    fireEvent.change(screen.getByLabelText('RCON password'), { target: { value: 'pw' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create server' }));
    await waitFor(() => expect(panel()).toBeNull());
    expect(creates(post)).toHaveLength(1);
  });

  it('a server admin sees no Save as New', async () => {
    setLanguage('en');
    await page('server_admin');
    expect(screen.queryByRole('button', { name: 'New server' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'old' }));
    expect(panel()).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Save as New' })).toBeNull();
  });
});
