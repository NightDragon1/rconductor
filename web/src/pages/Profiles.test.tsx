// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { profilesKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { mainNav, navContext, shown } from '@/nav';
import { makeRouter } from '@/router';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { ProfilesPage } from './Profiles';

vi.mock('@/lib/live', () => ({
  LiveSocket: class {
    close() {}
    subscribe() {
      return () => undefined;
    }
  },
}));

const now = new Date().toISOString();
const evil = '<img src=x onerror=alert(1)>';

function session(features: string[] = ['profile_browser'], role = 'moderator') {
  return {
    user: { id: 'u0', pseudonym: 'me', role },
    expires_at: now,
    must_change_password: false,
    totp_setup_required: false,
    panel_version: '1.2',
    ui: {
      features,
      shortcuts_enabled: false,
      supporters_enabled: false,
      supporters_allowed: false,
    },
  };
}

const base = {
  overridden: false,
  imported_at: now,
  case_sensitive: false,
  strip_prefix: '/',
  max_length: 256,
};

const profiles = [
  {
    ...base,
    id: 'mc',
    slug: 'minecraft-java',
    name: 'Minecraft',
    kind: 'generic',
    protocol: 'source-rcon',
    wrappers: ['execute'],
    catalog: [
      {
        path: 'kick',
        description: 'Kick a player',
        params: [
          { name: 'player', type: 'player' },
          { name: 'reason', type: 'text', optional: true },
        ],
      },
      {
        path: 'time set',
        description: 'Set the time',
        params: [{ name: 'value', type: 'number', min: 0, max: 24000 }],
      },
    ],
  },
  {
    ...base,
    id: 'pack',
    slug: 'pack',
    name: evil,
    kind: 'specific',
    protocol: 'source-rcon',
    wrappers: [],
    game_ports: [{ port: 25565, proto: 'tcp', name: 'game' }],
    catalog: [
      {
        path: 'difficulty',
        description: evil,
        params: [{ name: 'level', type: 'choice', values: ['peaceful', evil] }],
      },
    ],
  },
];

const servers = [
  { id: 's1', name: 'alpha', groups: [], status: 'connected', status_since: now, version: 'v' },
  { id: 's2', name: 'beta', groups: [], status: 'connected', status_since: now, version: 'v' },
];

const effective = {
  profile_ids: ['pack', 'mc'],
  protocol: 'source-rcon',
  case_sensitive: true,
  strip_prefix: '',
  max_length: 200,
  wrappers: ['execute'],
  separators: [';', '&&'],
  catalog: [
    { path: 'difficulty', description: 'Set it', params: [], profile_id: 'pack' },
    { path: 'kick', description: 'Kick a player', params: [], profile_id: 'mc' },
  ],
};

const data = (role = 'moderator'): [readonly unknown[], unknown][] => [
  [sessionKey, session(['profile_browser'], role)],
  [profilesKey, profiles],
  [serversKey, servers],
];

// mockProfile answers GET /servers/{id}/profile; other calls must not go out.
function mockProfile(answer: (id: string) => { status: number; body?: unknown }) {
  return vi.spyOn(api, 'GET').mockImplementation(((
    path: string,
    init: { params: { path: { id: string } } },
  ) => {
    if (path !== '/servers/{id}/profile') throw new Error(`unexpected GET ${path}`);
    const r = answer(init.params.path.id);
    const ok = r.status < 400;
    return Promise.resolve({
      data: ok ? r.body : undefined,
      error: ok ? undefined : { title: 'Not Found', status: r.status },
      response: new Response(null, { status: r.status }),
    });
  }) as never);
}

const pick = (id: string) =>
  fireEvent.change(screen.getByRole('combobox'), { target: { value: id } });

afterEach(() => {
  vi.restoreAllMocks();
  document.querySelector('base')?.remove();
  window.history.replaceState(null, '', '/');
});

describe('profile browser (UI-020)', () => {
  it('lists every profile with kind, protocol and the catalog with its parameters', async () => {
    setLanguage('en');
    await renderPage(ProfilesPage, data());
    const all = screen.getByRole('list', { name: 'All game profiles' });
    const items = within(all).getAllByTestId('profile');
    expect(items).toHaveLength(2);
    const mc = items[0] as HTMLElement;
    expect(mc.textContent).toContain('Minecraft');
    expect(mc.textContent).toContain('generic');
    expect(mc.textContent).toContain('source-rcon');
    expect(mc.textContent).toContain('2 commands');
    const catalog = within(mc).getByRole('list', { name: 'Commands of Minecraft' });
    expect(catalog.textContent).toContain('kick');
    expect(catalog.textContent).toContain('Kick a player');
    expect(catalog.textContent).toContain('reason');
    expect(catalog.textContent).toContain('optional');
    expect(catalog.textContent).toContain('min 0');
    expect(catalog.textContent).toContain('max 24000');
    const pack = items[1] as HTMLElement;
    expect(pack.textContent).toContain('specific');
    expect(pack.textContent).toContain('peaceful');
    expect(pack.textContent).toContain('choice');
  });

  it('shows game ports only where the backend sent them (D-056)', async () => {
    setLanguage('en');
    await renderPage(ProfilesPage, data());
    const [mc, pack] = screen.getAllByTestId('profile') as [HTMLElement, HTMLElement];
    expect(within(mc).queryByText('Game ports')).toBeNull();
    expect(within(pack).getByRole('list', { name: 'Game ports' }).textContent).toContain(
      '25565/tcp',
    );
  });

  it('shows foreign values as text, never as markup (SEC-006)', async () => {
    setLanguage('en');
    const { container } = await renderPage(ProfilesPage, data());
    expect(container.querySelector('img')).toBeNull();
    const pack = screen.getAllByTestId('profile')[1] as HTMLElement;
    // The name, the description and a value stand as text, each in a bdi of ForeignText.
    const texts = [...pack.querySelectorAll('bdi')].map((b) => b.textContent);
    expect(texts.filter((s) => s === evil).length).toBeGreaterThanOrEqual(3);
  });

  it('filters by profile name and by command path or description', async () => {
    setLanguage('en');
    await renderPage(ProfilesPage, data());
    const search = screen.getByRole('searchbox', { name: /Search game profiles/ });
    fireEvent.change(search, { target: { value: 'KICK' } });
    await waitFor(() => expect(screen.getAllByTestId('profile')).toHaveLength(1));
    const mc = screen.getByTestId('profile');
    expect(within(mc).getAllByTestId('catalog-entry')).toHaveLength(1);
    fireEvent.change(search, { target: { value: 'set the' } });
    await waitFor(() => expect(screen.getByTestId('profile').textContent).toContain('time set'));
    fireEvent.change(search, { target: { value: 'minecraft' } });
    await waitFor(() => expect(within(mc).getAllByTestId('catalog-entry')).toHaveLength(2));
    fireEvent.change(search, { target: { value: 'nothing like this' } });
    await waitFor(() => expect(screen.getByText('Nothing matches.')).toBeTruthy());
  });

  it('takes a search of 10 KiB with control characters and regex syntax', async () => {
    setLanguage('en');
    const errors = vi.spyOn(console, 'error');
    await renderPage(ProfilesPage, data());
    const controls = '\u0000\u0007\u001b[31m‮(.*)+[\\\n\r\t';
    const big = (controls + 'a'.repeat(1000)).repeat(11).slice(0, 10 * 1024);
    expect(big.length).toBe(10 * 1024);
    const search = screen.getByRole('searchbox', { name: /Search game profiles/ });
    const start = performance.now();
    fireEvent.change(search, { target: { value: big } });
    await waitFor(() => expect(screen.getByText('Nothing matches.')).toBeTruthy());
    expect(performance.now() - start).toBeLessThan(2000);
    expect(errors).not.toHaveBeenCalled();
    fireEvent.change(search, { target: { value: '' } });
    await waitFor(() => expect(screen.getAllByTestId('profile')).toHaveLength(2));
  });

  it('shows the effective profile of a chosen server with the source of each command', async () => {
    setLanguage('en');
    const get = mockProfile((id) => ({ status: id === 's1' ? 200 : 404, body: effective }));
    await renderPage(ProfilesPage, data());
    expect(get).not.toHaveBeenCalled();
    pick('s1');
    const view = await screen.findByTestId('effective');
    expect(get).toHaveBeenCalledTimes(1);
    const order = within(view).getByRole('list', { name: 'Profiles, top first' });
    expect([...order.querySelectorAll('li')].map((li) => li.textContent)).toEqual([
      evil,
      'Minecraft',
    ]);
    expect(view.textContent).toContain('Case sensitiveyes');
    expect(view.textContent).toContain('200');
    expect(view.textContent).toContain('&&');
    expect(view.textContent).toContain('execute');
    // Without game ports in the answer there is no such field.
    expect(within(view).queryByText('Game ports')).toBeNull();
    const entries = within(view).getAllByTestId('catalog-entry');
    expect(entries[0]?.textContent).toContain(`from ${evil}`);
    expect(entries[1]?.textContent).toContain('from Minecraft');
    expect(view.querySelector('img')).toBeNull();
  });

  it('shows the effective game ports when the answer carries them', async () => {
    setLanguage('en');
    mockProfile(() => ({
      status: 200,
      body: { ...effective, game_ports: [{ port: 27015, proto: 'udp', name: 'query' }] },
    }));
    await renderPage(ProfilesPage, data('server_admin'));
    pick('s1');
    const view = await screen.findByTestId('effective');
    expect(within(view).getByRole('list', { name: 'Game ports' }).textContent).toContain(
      '27015/udp',
    );
  });

  it('answers a 404 with a note and asks only once', async () => {
    setLanguage('en');
    const get = mockProfile(() => ({ status: 404 }));
    await renderPage(ProfilesPage, data());
    pick('s2');
    expect(await screen.findByText('There is no effective profile for this server.')).toBeTruthy();
    expect(get).toHaveBeenCalledTimes(1);
  });

  it('has no control that writes', async () => {
    setLanguage('en');
    mockProfile(() => ({ status: 200, body: effective }));
    const { container } = await renderPage(ProfilesPage, data('rcon_admin'));
    pick('s1');
    await screen.findByTestId('effective');
    expect(container.querySelectorAll('form, button, textarea, [contenteditable]')).toHaveLength(0);
    const inputs = [...container.querySelectorAll('input')].map((i) => i.type);
    expect(inputs).toEqual(['search']);
    expect(container.querySelectorAll('select')).toHaveLength(1);
  });

  it('takes every text from the language file', async () => {
    setLanguage('xx');
    mockProfile(() => ({ status: 200, body: effective }));
    const { container } = await renderPage(ProfilesPage, data());
    pick('s1');
    await screen.findByTestId('effective');
    // Data stands in bdi; the server names in the picker are data as well.
    const names = new Set(['alpha', 'beta']);
    const texts = unmarked(container).filter((s) => !names.has(s));
    const data_ = new Set([...container.querySelectorAll('bdi')].map((b) => b.textContent?.trim()));
    expect(texts.filter((s) => !data_.has(s) && /\p{L}/u.test(s))).toEqual([]);
    setLanguage('en');
  });
});

describe('profile browser behind the flag profile_browser (UI-020, NFR-019)', () => {
  it('has its navigation entry only while the flag is on', () => {
    const entry = mainNav.find((e) => e.to === '/profiles');
    if (!entry) throw new Error('no navigation entry /profiles');
    for (const role of ['rcon_admin', 'server_admin', 'moderator', 'supporter'] as const) {
      expect(shown(entry, role, navContext(session([]).ui))).toBe(false);
      expect(shown(entry, role, navContext(session().ui))).toBe(true);
    }
  });

  async function open(features: string[]) {
    setLanguage('en');
    const get = vi.spyOn(api, 'GET').mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/auth/session'
            ? session(features)
            : path === '/profiles'
              ? { items: profiles }
              : path === '/servers'
                ? { items: servers }
                : undefined,
        response: new Response(null, { status: 200 }),
      })) as never);
    // The router takes its base path from <base href> (issue #88).
    const b = document.createElement('base');
    b.href = '/';
    document.head.append(b);
    window.history.replaceState(null, '', '/profiles');
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = makeRouter(qc);
    render(
      <QueryClientProvider client={qc}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await act(() => router.load());
    return get;
  }

  it('does not exist while the flag is off: no menu entry, no page, no profile request', async () => {
    const get = await open([]);
    expect(await screen.findByText('Page not found')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Game profiles' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Game profiles' })).toBeNull();
    expect(get.mock.calls.map((c) => c[0])).not.toContain('/profiles');
  });

  it('shows menu entry and page while the flag is on', async () => {
    await open(['profile_browser']);
    expect(await screen.findByRole('heading', { name: 'Game profiles' })).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'Game profiles' }).length).toBeGreaterThan(0);
  });
});
