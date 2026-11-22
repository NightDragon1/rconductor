// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { agentsKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { dateTime } from '@/lib/format';
import { makeRouter } from '@/router';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { SystemLogPage } from './SystemLog';

const systemLogKey = ['system-log'];

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
const flag = 'diagnostics_export';

function session(role = 'rcon_admin', features = [flag]) {
  return {
    user: { id: 'me', pseudonym: 'me', role },
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

const agent = (id: string, status = 'active') => ({
  id,
  name: `agent ${id}`,
  fingerprint: 'SHA256:x',
  status,
  version: '1',
  capabilities: [],
  connected: true,
  created_at: now,
});

const entry = (id: number, over: object = {}) => ({
  id,
  time: now,
  level: 'info',
  code: 'PANEL-001',
  text: `line ${id}`,
  ...over,
});

type Answer = { status: number; body?: unknown };
const answer = ({ status, body }: Answer) => {
  const ok = status < 400;
  return Promise.resolve({
    data: ok ? body : undefined,
    error: ok ? undefined : body,
    response: new Response(null, { status }),
  });
};

type Init = { params?: { query?: Record<string, unknown> } };

// mock answers the system log by a function of its query.
function mock(log: (q: Record<string, unknown>) => Answer) {
  return vi.spyOn(api, 'GET').mockImplementation(((path: string, init?: Init) => {
    if (path === '/system-log') return answer(log(init?.params?.query ?? {}));
    throw new Error(`unexpected GET ${path}`);
  }) as never);
}

const page = (items: unknown[], source = 'panel') => ({ status: 200, body: { source, items } });
const agents = [agent('a1'), agent('a2', 'revoked')];

function open() {
  return renderPage(SystemLogPage, [
    [sessionKey, session()],
    [agentsKey, agents],
  ]);
}

const logCalls = (get: ReturnType<typeof mock>) =>
  get.mock.calls.filter((c) => c[0] === '/system-log').map((c) => (c[1] as Init).params?.query);

afterEach(() => {
  vi.restoreAllMocks();
  document.querySelector('base')?.remove();
  window.history.replaceState(null, '', '/');
});

describe('system log (UI-025, D-091)', () => {
  it('reads the panel log of the last hour from info up and shows the notice', async () => {
    setLanguage('en');
    const get = mock(() => page([entry(1, { level: 'warn' })]));
    await open();
    const list = await screen.findByRole('region', { name: 'Entries' });
    await within(list).findByText('line 1');
    expect(within(list).getByText('warn')).toBeTruthy();
    expect(within(list).getByText('PANEL-001')).toBeTruthy();
    expect(screen.getByRole('note').textContent).toBe('May contain chat messages of players.');
    const q = logCalls(get)[0];
    expect(q).toMatchObject({ source: 'panel', level: 'info', limit: 500 });
    const from = Date.parse(String(q?.from));
    expect(Date.now() - from).toBeGreaterThanOrEqual(3_600_000 - 1000);
    expect(Date.now() - from).toBeLessThan(3_600_000 + 60_000);
    // to from the same clock, so the period is exact (7 days stay valid).
    expect(Date.parse(String(q?.to)) - from).toBe(3_600_000);
    expect(q).not.toHaveProperty('after');
  });

  it('builds the request from level and source; only active agents are sources', async () => {
    setLanguage('en');
    const get = mock((q) => page([], String(q.source)));
    await open();
    await screen.findByText('No entries.');
    const source = screen.getByLabelText('Source') as HTMLSelectElement;
    expect([...source.options].map((o) => o.value)).toEqual(['panel', 'a1']);
    fireEvent.change(screen.getByLabelText('Level'), { target: { value: 'error' } });
    await waitFor(() =>
      expect(logCalls(get).at(-1)).toMatchObject({ source: 'panel', level: 'error' }),
    );
    fireEvent.change(source, { target: { value: 'a1' } });
    await waitFor(() => expect(logCalls(get).at(-1)).toMatchObject({ source: 'a1' }));
    // From, to and after do not apply to an agent.
    const q = logCalls(get).at(-1);
    expect(q).toEqual({ source: 'a1', level: 'error', limit: 500 });
    expect(
      await screen.findByText(
        'The log of an agent shows its last lines; the period applies to the export.',
      ),
    ).toBeTruthy();
  });

  it('polls after the last entry seen and appends', async () => {
    setLanguage('en');
    const get = mock((q) => (q.after ? page([entry(3)]) : page([entry(1), entry(2)])));
    const { qc } = await open();
    await screen.findByText('line 2');
    await act(() => qc.refetchQueries({ queryKey: systemLogKey }));
    await screen.findByText('line 3');
    expect(logCalls(get).at(-1)).toEqual({ source: 'panel', level: 'info', after: 2, limit: 500 });
    expect(screen.getByText('line 1')).toBeTruthy();
  });

  it('polls on its own', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      setLanguage('en');
      const get = mock(() => page([entry(1)]));
      await open();
      await screen.findByText('line 1');
      const before = logCalls(get).length;
      await act(() => vi.advanceTimersByTimeAsync(5000));
      await waitFor(() => expect(logCalls(get).length).toBeGreaterThan(before));
      expect(logCalls(get).at(-1)).toMatchObject({ after: 1 });
    } finally {
      vi.useRealTimers();
    }
  });

  it('shows an agent offline (503) and an agent without the capability (409)', async () => {
    setLanguage('en');
    let status = 503;
    mock((q) =>
      q.source === 'panel'
        ? page([])
        : { status, body: { type: 'about:blank', title: 'Service Unavailable', status } },
    );
    const { qc } = await open();
    await screen.findByText('No entries.');
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'a1' } });
    expect((await screen.findByRole('alert')).textContent).toBe('The agent is offline.');
    status = 409;
    await act(() => qc.refetchQueries({ queryKey: systemLogKey }));
    await waitFor(() =>
      expect(screen.getByRole('alert').textContent).toBe('This agent cannot send its log.'),
    );
  });

  it('shows the backend reason while the panel log is not built (501)', async () => {
    setLanguage('en');
    mock(() => ({
      status: 501,
      body: { type: 'about:blank', title: 'Not Implemented', status: 501 },
    }));
    await open();
    expect((await screen.findByRole('alert')).textContent).toBe('Failed: Not Implemented');
  });

  it('downloads the package for the chosen period and names a refusal', async () => {
    setLanguage('en');
    const zip = new Blob(['PK'], { type: 'application/zip' });
    let status = 200;
    const get = vi
      .spyOn(api, 'GET')
      .mockImplementation(((path: string) =>
        path === '/diagnostics'
          ? answer(
              status === 200
                ? { status, body: zip }
                : { status, body: { type: 'about:blank', title: 'Busy', status } },
            )
          : answer(page([]))) as never);
    const create = vi.fn(() => 'blob:x');
    const revoke = vi.fn();
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    await open();
    await screen.findByText('No entries.');
    fireEvent.change(screen.getByLabelText('Period'), { target: { value: 'week' } });
    const button = screen.getByRole('button', { name: 'Download diagnostic package' });
    expect(button.getAttribute('aria-describedby')).toBe(screen.getByRole('note').id);
    fireEvent.click(button);
    await waitFor(() => expect(click).toHaveBeenCalled());
    expect(create).toHaveBeenCalledWith(zip);
    expect(revoke).toHaveBeenCalledWith('blob:x');
    const call = get.mock.calls.find((c) => c[0] === '/diagnostics');
    const init = call?.[1] as unknown as Init & { parseAs?: string };
    expect(init.parseAs).toBe('blob');
    const q = init.params?.query as { from: string; to: string };
    expect(Date.parse(q.to) - Date.parse(q.from)).toBe(7 * 86_400_000);

    status = 503;
    fireEvent.click(button);
    expect((await screen.findByRole('alert')).textContent).toBe('Failed: Busy');
  });

  it('shows lines as text through ForeignText (SEC-006)', async () => {
    setLanguage('en');
    mock(() => page([entry(1, { text: evil }), entry(2, { text: 'a\u202Eb' })]));
    const { container } = await open();
    await screen.findByText(evil);
    expect(container.querySelector('img')).toBeNull();
    // ForeignText isolates each line and shows a bidi control instead of letting it act.
    const list = screen.getByRole('region', { name: 'Entries' });
    expect(list.querySelector('bdi[data-code="⟨U+202E⟩"]')).not.toBeNull();
  });

  it('takes every text from the language file', async () => {
    setLanguage('xx');
    mock(() => page([entry(1, { level: 'error' })]));
    const { container } = await open();
    await screen.findByText('line 1');
    // Agent names, codes, texts and times are data.
    const texts = unmarked(container).filter(
      (s) =>
        !['agent a1', 'PANEL-001', 'line 1', dateTime('xx', now)].includes(s) && /\p{L}/u.test(s),
    );
    expect(texts).toEqual([]);
    setLanguage('en');
  });
});

describe('system log in the menu (NFR-018)', () => {
  async function at(role: string, features: string[]) {
    setLanguage('en');
    const get = vi.spyOn(api, 'GET').mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/auth/session'
            ? session(role, features)
            : path === '/system-log'
              ? { source: 'panel', items: [] }
              : { items: [] },
        response: new Response(null, { status: 200 }),
      })) as never);
    const b = document.createElement('base');
    b.href = '/';
    document.head.append(b);
    window.history.replaceState(null, '', '/admin/system-log');
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

  it('does not exist for a server admin', async () => {
    const get = await at('server_admin', [flag]);
    expect(await screen.findByText('Page not found')).toBeTruthy();
    expect(screen.queryByRole('link', { name: /System log/ })).toBeNull();
    expect(get.mock.calls.map((c) => c[0])).not.toContain('/system-log');
  });

  it('shows menu entry and page to the RCON admin', async () => {
    await at('rcon_admin', [flag]);
    expect(await screen.findByRole('heading', { name: 'System log' })).toBeTruthy();
    expect((await screen.findAllByRole('link', { name: /System log/ })).length).toBeGreaterThan(0);
  });
});
