// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { dateTime } from '@/lib/format';
import { RouterProvider } from '@tanstack/react-router';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { sessionKey } from '@/api/session';
import { makeRouter } from '@/router';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { messageTitle } from '@/api/messages';
import { MessagesPage } from './Messages';

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

function session(role = 'rcon_admin', features = ['messages']) {
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

const msg = (id: string, over: object = {}) => ({
  id,
  sender: { id: 'boss', pseudonym: 'boss' },
  text: `text ${id}`,
  audience: 'all',
  created_at: now,
  comment_count: 0,
  read: true,
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

// mock answers GET by path; every other call goes to write.
function mock(
  get: Record<string, Answer>,
  write: (method: string, path: string, init: unknown) => Answer = () => ({ status: 204 }),
) {
  const g = vi.spyOn(api, 'GET').mockImplementation(((path: string) => {
    const a = get[path];
    if (!a) throw new Error(`unexpected GET ${path}`);
    return answer(a);
  }) as never);
  const p = vi
    .spyOn(api, 'POST')
    .mockImplementation(((path: string, init: unknown) =>
      answer(write('POST', path, init))) as never);
  const d = vi
    .spyOn(api, 'DELETE')
    .mockImplementation(((path: string, init: unknown) =>
      answer(write('DELETE', path, init))) as never);
  return { get: g, post: p, del: d };
}

const list = (items: unknown[], unread = 0) => ({ status: 200, body: { items, unread } });

// openMessage chooses the first conversation of the list.
async function openMessage() {
  fireEvent.click(within(await screen.findByTestId('message')).getByRole('button'));
  return screen.findByRole('article');
}

// compose opens the form of a new message in its dialog.
function compose() {
  fireEvent.click(screen.getByRole('button', { name: 'New message' }));
  return screen.getByRole('form', { name: 'New message' });
}
const noComments = { status: 200, body: { items: [] } };

afterEach(() => {
  vi.restoreAllMocks();
  document.querySelector('base')?.remove();
  window.history.replaceState(null, '', '/');
});

describe('messages (MSG-001 to MSG-003, D-085)', () => {
  it('lets the RCON admin send to chosen users and shows the recipients', async () => {
    setLanguage('en');
    const calls = mock(
      {
        '/messages': list([msg('m1', { sender: { id: 'me', pseudonym: 'me' } })]),
        '/users': {
          status: 200,
          body: { items: [{ id: 'u1', pseudonym: 'mod', role: 'moderator' }] },
        },
      },
      () => ({ status: 201, body: { ...msg('m2'), recipient_count: 1 } }),
    );
    await renderPage(MessagesPage, [[sessionKey, session()]]);
    expect(screen.queryByRole('form', { name: 'New message' })).toBeNull();
    const form = compose();
    expect(within(screen.getByRole('dialog', { name: 'New message' })).getByRole('form')).toBe(
      form,
    );
    const to = within(form).getByLabelText('To') as HTMLSelectElement;
    expect([...to.options].map((o) => o.value)).toEqual([
      'all',
      'server_admins',
      'moderators',
      'users',
    ]);
    fireEvent.change(to, { target: { value: 'users' } });
    fireEvent.click(await within(form).findByRole('checkbox', { name: /mod/ }));
    fireEvent.change(within(form).getByLabelText('Text'), { target: { value: 'Hello' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Send' }));
    await waitFor(() =>
      expect(within(form).getByRole('status').textContent).toBe('Sent to 1 recipient.'),
    );
    expect(calls.post).toHaveBeenCalledWith('/messages', {
      body: { text: 'Hello', audience: 'users', user_ids: ['u1'] },
    });
  });

  it('offers a server admin only his scope', async () => {
    setLanguage('en');
    mock({ '/messages': list([]) });
    await renderPage(MessagesPage, [[sessionKey, session('server_admin')]]);
    const to = within(compose()).getByLabelText('To') as HTMLSelectElement;
    expect([...to.options].map((o) => o.value)).toEqual(['scope', 'scope_moderators']);
    expect([...to.options].map((o) => o.text)).toEqual([
      'Everyone on my servers',
      'Moderators on my servers',
    ]);
  });

  it('gives a moderator no send form and marks an opened message read', async () => {
    setLanguage('en');
    const calls = mock({
      '/messages': list([msg('m1', { read: false, text: evil })], 1),
      '/messages/{id}/comments': noComments,
    });
    const { container } = await renderPage(MessagesPage, [[sessionKey, session('moderator')]]);
    await screen.findByTestId('message');
    expect(screen.queryByRole('button', { name: 'New message' })).toBeNull();
    // The text is foreign data: text, never markup (SEC-006).
    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByTestId('message').textContent).toContain(evil);
    // Unread is bold in the list.
    expect(within(screen.getByTestId('message')).getByRole('button').className).toContain(
      'font-semibold',
    );
    await openMessage();
    await waitFor(() =>
      expect(calls.post).toHaveBeenCalledWith('/messages/{id}/read', {
        params: { path: { id: 'm1' } },
      }),
    );
    // Not the sender and not the RCON admin: no delete.
    expect(screen.queryByRole('button', { name: 'Delete message' })).toBeNull();
  });

  it('does not mark a message read that I sent and lets me delete it', async () => {
    setLanguage('en');
    const calls = mock({
      '/messages': list([msg('m1', { sender: { id: 'me', pseudonym: 'me' }, recipient_count: 4 })]),
      '/messages/{id}/comments': noComments,
    });
    await renderPage(MessagesPage, [[sessionKey, session('server_admin')]]);
    const view = await openMessage();
    expect(within(view).getByText('4 recipients')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Delete message' }));
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete message' }),
    );
    await waitFor(() =>
      expect(calls.del).toHaveBeenCalledWith('/messages/{id}', { params: { path: { id: 'm1' } } }),
    );
    // The view closes with the deleted message.
    await waitFor(() => expect(screen.queryByRole('article')).toBeNull());
    expect(calls.post).not.toHaveBeenCalled();
  });

  it('shows the refusal of the backend instead of a success', async () => {
    setLanguage('en');
    mock({ '/messages': list([]) }, () => ({
      status: 429,
      body: { title: 'Too Many Requests', status: 429, detail: 'too many messages this hour' },
    }));
    await renderPage(MessagesPage, [[sessionKey, session()]]);
    const form = compose();
    fireEvent.change(within(form).getByLabelText('Text'), { target: { value: 'Hello' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Send' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Failed: too many messages this hour',
    );
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('shows the error text of the comments and sends a comment', async () => {
    setLanguage('en');
    const calls = mock(
      {
        '/messages': list([msg('m1')]),
        '/messages/{id}/comments': {
          status: 501,
          body: { title: 'Not Implemented', status: 501 },
        },
      },
      () => ({ status: 501, body: { title: 'Not Implemented', status: 501 } }),
    );
    await renderPage(MessagesPage, [[sessionKey, session('supporter')]]);
    await openMessage();
    const comments = await screen.findByRole('region', { name: 'Comments' });
    expect((await within(comments).findByRole('alert')).textContent).toBe(
      'Failed: Not Implemented',
    );
    fireEvent.change(within(comments).getByLabelText('Your reply'), {
      target: { value: 'ok' },
    });
    fireEvent.click(within(comments).getByRole('button', { name: 'Reply' }));
    await waitFor(() =>
      expect(calls.post).toHaveBeenCalledWith('/messages/{id}/comments', {
        params: { path: { id: 'm1' } },
        body: { text: 'ok' },
      }),
    );
  });

  it('deletes a comment only where author, sender or RCON admin', async () => {
    setLanguage('en');
    mock({
      '/messages': list([msg('m1')]),
      '/messages/{id}/comments': {
        status: 200,
        body: {
          items: [
            { id: 'c1', author: { id: 'me', pseudonym: 'me' }, text: 'mine', created_at: now },
            { id: 'c2', author: { id: 'x', pseudonym: 'x' }, text: evil, created_at: now },
          ],
        },
      },
    });
    const { container } = await renderPage(MessagesPage, [[sessionKey, session('moderator')]]);
    await openMessage();
    const [own, other] = (await screen.findAllByTestId('comment')) as [HTMLElement, HTMLElement];
    expect(within(own).queryByRole('button', { name: 'Delete' })).toBeTruthy();
    expect(within(other).queryByRole('button', { name: 'Delete' })).toBeNull();
    expect(other.textContent).toContain(evil);
    expect(container.querySelector('img')).toBeNull();
  });

  it('titles a message by its first line and leads back to the list', async () => {
    setLanguage('en');
    mock({
      '/messages': list([msg('m1', { text: '\n  Restart at 8\nDetails follow', read: false })]),
      '/messages/{id}/comments': noComments,
    });
    await renderPage(MessagesPage, [[sessionKey, session('supporter')]]);
    const item = await screen.findByTestId('message');
    expect(within(item).getByText('Restart at 8')).toBeTruthy();
    expect(item.textContent).not.toContain('Details follow');
    // Nothing chosen yet: a hint stands for the message.
    expect(screen.getByText('No message selected.')).toBeTruthy();
    expect(screen.getByText('Choose a message on the left to read it.')).toBeTruthy();
    const view = await openMessage();
    expect(view.textContent).toContain('Details follow');
    expect(within(item).getByRole('button').getAttribute('aria-current')).toBe('true');
    fireEvent.click(within(view).getByRole('button', { name: 'Back to messages' }));
    expect(screen.queryByRole('article')).toBeNull();
    expect(within(item).getByRole('button').getAttribute('aria-current')).toBeNull();
  });

  it('names an empty list and no selection in words, without placeholder bars', async () => {
    setLanguage('en');
    mock({ '/messages': list([]) });
    const { container } = await renderPage(MessagesPage, [[sessionKey, session()]]);
    expect(await screen.findByText('No messages yet.')).toBeTruthy();
    expect(screen.getByText('No message selected.')).toBeTruthy();
    expect(screen.getByText('Choose a message on the left to read it.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'New message' })).toBeTruthy();
    expect(screen.queryByRole('list')).toBeNull();
    expect(screen.queryByText('Loading…')).toBeNull();
    expect(container.querySelector('[aria-hidden="true"] .rounded')).toBeNull();
  });

  it('shows only a short hint while the list loads', async () => {
    setLanguage('en');
    vi.spyOn(api, 'GET').mockImplementation((() => new Promise(() => undefined)) as never);
    const { container } = await renderPage(MessagesPage, [[sessionKey, session('moderator')]]);
    expect(await screen.findByText('Loading…')).toBeTruthy();
    expect(screen.queryByText('No messages yet.')).toBeNull();
    expect(container.querySelector('[aria-hidden="true"] .rounded')).toBeNull();
  });

  it('cuts a long first line and keeps a character whole', () => {
    expect(messageTitle('a'.repeat(5000))).toBe(`${'a'.repeat(120)}…`);
    expect(messageTitle('x'.repeat(119) + '😀😀')).toBe(`${'x'.repeat(119)}😀…`);
    expect(messageTitle('short')).toBe('short');
    expect(messageTitle('\n\n')).toBe('');
  });

  it('loads older messages with the cursor of the backend (MSG-001)', async () => {
    setLanguage('en');
    const page = Array.from({ length: 50 }, (_, i) => msg(`m${i}`));
    const calls = mock({
      '/messages': { status: 200, body: { items: page, unread: 0, next_cursor: 'c1' } },
    });
    await renderPage(MessagesPage, [[sessionKey, session('supporter')]]);
    fireEvent.click(await screen.findByRole('button', { name: 'Load more' }));
    await waitFor(() =>
      expect(calls.get).toHaveBeenCalledWith('/messages', {
        params: { query: { limit: 50, cursor: 'c1' } },
      }),
    );
  });

  it('offers no more after a last page of exactly 50 (MSG-001)', async () => {
    setLanguage('en');
    const page = Array.from({ length: 50 }, (_, i) => msg(`m${i}`));
    mock({ '/messages': list(page) });
    await renderPage(MessagesPage, [[sessionKey, session('supporter')]]);
    await screen.findAllByTestId('message');
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('takes every text from the language file', async () => {
    setLanguage('xx');
    mock({
      '/messages': list([msg('m1', { read: false })], 1),
      '/messages/{id}/comments': noComments,
    });
    const { container } = await renderPage(MessagesPage, [[sessionKey, session()]]);
    await openMessage();
    await screen.findByRole('region');
    // Names, texts and times are data.
    const texts = unmarked(container).filter(
      (s) => !['boss', 'text m1', dateTime('xx', now)].includes(s) && /\p{L}/u.test(s),
    );
    expect(texts).toEqual([]);
    setLanguage('en');
  });
});

describe('messages in the menu (MSG-001)', () => {
  async function open(features: string[]) {
    setLanguage('en');
    const get = vi.spyOn(api, 'GET').mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/auth/session'
            ? session('moderator', features)
            : path === '/messages'
              ? { items: [], unread: 3 }
              : { items: [] },
        response: new Response(null, { status: 200 }),
      })) as never);
    const b = document.createElement('base');
    b.href = '/';
    document.head.append(b);
    window.history.replaceState(null, '', '/messages');
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

  it('shows the menu entry with the unread count', async () => {
    await open(['messages']);
    expect(await screen.findByRole('heading', { name: 'Messages' })).toBeTruthy();
    expect(
      (await screen.findAllByRole('link', { name: /Messages.*3 unread messages/ })).length,
    ).toBeGreaterThan(0);
  });
});
