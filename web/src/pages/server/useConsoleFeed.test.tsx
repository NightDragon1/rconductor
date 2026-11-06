// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import {
  addCommands,
  bufferOwner,
  emptyBuffer,
  resetBuffers,
  updateBuffer,
  useBuffer,
} from '@/lib/console';
import { useConsoleFeed } from './useConsoleFeed';

vi.mock('@/api/session', () => ({ useSession: () => ({ data: undefined }) }));

const topics = vi.hoisted(() => new Map<string, (m: { type: string }) => void>());
vi.mock('@/lib/live-context', () => ({
  useTopic: (topic: string | undefined, listener: (m: { type: string }) => void) => {
    if (topic) topics.set(topic, listener);
  },
}));

const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const page = { data: { items: [], next_cursor: '' }, response: new Response() };

const fromEntry = (n: number) => ({
  kind: 'cmd' as const,
  id: `r${n}`,
  ts: new Date(n * 1000).toISOString(),
  command: 'say',
  result: 'ok' as const,
});

function feed() {
  return renderHook(
    ({ id }) => ({ feed: useConsoleFeed(id, false), a: useBuffer('a'), b: useBuffer('b') }),
    { wrapper, initialProps: { id: 'a' } },
  );
}

describe('useConsoleFeed (CMD-001)', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    bufferOwner(undefined);
    resetBuffers();
    client.clear();
  });

  it('keeps an answer with the server it was sent to', async () => {
    let answer: (v: unknown) => void = () => undefined;
    vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    const post = vi.spyOn(api, 'POST').mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve as (v: unknown) => void;
        }) as never,
    );
    const { result, rerender } = feed();
    act(() => result.current.feed.send.mutate('stop'));
    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(result.current.feed.send.isPending).toBe(true);

    rerender({ id: 'b' });
    expect(result.current.feed.send.isPending).toBe(false);
    act(() =>
      answer({
        data: { request_id: 'r1', ts: '2026-11-06T10:00:00Z', command: 'stop', response: 'ok' },
        response: new Response(),
      }),
    );
    await waitFor(() => expect(result.current.a.cmds.map((c) => c.id)).toEqual(['r1']));
    expect(result.current.b.cmds).toEqual([]);
  });

  it('keeps the answer of a command from the next user', async () => {
    let answer: (v: unknown) => void = () => undefined;
    bufferOwner('alice');
    vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    const post = vi.spyOn(api, 'POST').mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve as (v: unknown) => void;
        }) as never,
    );
    const { result } = feed();
    act(() => result.current.feed.send.mutate('secret'));
    await waitFor(() => expect(post).toHaveBeenCalled());

    act(() => bufferOwner('bob'));
    act(() =>
      answer({
        data: { request_id: 'r9', ts: '2026-11-06T10:00:00Z', command: 'secret', response: 'x' },
        response: new Response(),
      }),
    );
    await waitFor(() => expect(result.current.feed.send.isPending).toBe(false));
    await new Promise((r) => setTimeout(r));
    expect(result.current.a.cmds).toEqual([]);
    expect(result.current.a.running ?? 0).toBe(0);
  });

  it('keeps the refusal of a command from the next user', async () => {
    let refuse: (v: unknown) => void = () => undefined;
    bufferOwner('alice');
    vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    const post = vi.spyOn(api, 'POST').mockImplementation(
      () =>
        new Promise((_, reject) => {
          refuse = reject as (v: unknown) => void;
        }) as never,
    );
    const { result } = feed();
    act(() => result.current.feed.send.mutate('secret'));
    await waitFor(() => expect(post).toHaveBeenCalled());

    act(() => bufferOwner('bob'));
    act(() => refuse(new Error('gone')));
    await new Promise((r) => setTimeout(r));
    expect(result.current.a.cmds).toEqual([]);
  });

  it('keeps the history of a user from the next user', async () => {
    let page1: (v: unknown) => void = () => undefined;
    bufferOwner('alice');
    const get = vi.spyOn(api, 'GET').mockImplementation(
      () =>
        new Promise((resolve) => {
          page1 = resolve as (v: unknown) => void;
        }) as never,
    );
    const { result } = feed();
    await waitFor(() => expect(get).toHaveBeenCalled());

    act(() => bufferOwner('bob'));
    act(() =>
      page1({
        data: {
          items: [
            { request_id: 'a1', ts: '2026-11-06T10:00:00Z', command: 'secret', result: 'ok' },
          ],
          next_cursor: '',
        },
        response: new Response(),
      }),
    );
    await new Promise((r) => setTimeout(r));
    expect(result.current.a.cmds).toEqual([]);
  });

  it('leaves no gap after more commands than a page while away', async () => {
    const entry = (n: number) => ({
      request_id: `r${n}`,
      ts: new Date(n * 1000).toISOString(),
      command: 'say',
      result: 'ok',
    });
    const get = vi.spyOn(api, 'GET');
    get.mockResolvedValue({
      data: { items: [entry(10)], next_cursor: 'old' },
      response: new Response(),
    } as never);
    const { result, rerender } = feed();
    await waitFor(() => expect(result.current.a.cmds).toHaveLength(1));
    rerender({ id: 'b' });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    get.mockResolvedValue({
      data: { items: [entry(100)], next_cursor: 'new' },
      response: new Response(),
    } as never);
    rerender({ id: 'a' });
    await waitFor(() => expect(result.current.a.cmds.map((c) => c.id)).toEqual(['r100']));
    expect(result.current.a.cursor).toBe('new');
  });

  const entry = (n: number) => ({
    request_id: `r${n}`,
    ts: new Date(n * 1000).toISOString(),
    command: 'say',
    result: 'ok',
  });
  const answer = (items: unknown[], next_cursor: string) =>
    ({ data: { items, next_cursor }, response: new Response() }) as never;

  it('takes the cursor of the newest page even if a command of it is known', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(answer([entry(10)], 'old'));
    const { result } = feed();
    await waitFor(() => expect(result.current.a.cmds).toHaveLength(1));
    // The own command of the outage is in the buffer and in the new page.
    updateBuffer('a', (b) => addCommands(b, [fromEntry(100)]));
    get.mockResolvedValue(answer([entry(100)], 'new'));
    await act(() => client.invalidateQueries());
    await waitFor(() => expect(result.current.a.cursor).toBe('new'));
    expect(result.current.a.cmds.map((c) => c.id)).toEqual(['r100']);
  });

  it('keeps live commands newer than the page', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(answer([entry(10)], 'old'));
    const { result } = feed();
    await waitFor(() => expect(result.current.a.cmds).toHaveLength(1));
    updateBuffer('a', (b) => addCommands(b, [fromEntry(200)]));
    get.mockResolvedValue(answer([entry(100)], 'new'));
    await act(() => client.invalidateQueries());
    await waitFor(() => expect(result.current.a.cursor).toBe('new'));
    expect(result.current.a.cmds.map((c) => c.id)).toEqual(['r100', 'r200']);
  });

  it('drops an older page that comes after a refill', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(answer([entry(10)], 'old'));
    const { result } = feed();
    await waitFor(() => expect(result.current.a.cursor).toBe('old'));
    let older: (v: unknown) => void = () => undefined;
    get.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          older = resolve as (v: unknown) => void;
        }) as never,
    );
    let pending: Promise<unknown> = Promise.resolve();
    act(() => {
      pending = result.current.feed.loadOlder();
    });
    get.mockResolvedValue(answer([entry(100)], 'new'));
    await act(() => client.invalidateQueries());
    await waitFor(() => expect(result.current.a.cursor).toBe('new'));
    await act(async () => {
      older(answer([entry(1)], ''));
      await pending;
    });
    expect(result.current.a.cursor).toBe('new');
    expect(result.current.a.cmds.map((c) => c.id)).toEqual(['r100']);
  });

  it('fetches again once the subscription is confirmed', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    feed();
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    act(() => topics.get('server.a.console')?.({ type: 'subscribed' }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
  });

  it('applies only the refill asked for last', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(answer([entry(1)], 'c1'));
    const { result } = feed();
    await waitFor(() => expect(result.current.a.cursor).toBe('c1'));
    let early: (v: unknown) => void = () => undefined;
    get.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          early = resolve as (v: unknown) => void;
        }) as never,
    );
    act(() => topics.get('server.a.console')?.({ type: 'subscribed' }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    get.mockResolvedValue(answer([entry(200)], 'c200'));
    act(() => topics.get('server.a.console')?.({ type: 'subscribed' }));
    await waitFor(() => expect(result.current.a.cursor).toBe('c200'));
    await act(async () => {
      early(answer([entry(100)], 'c100'));
      await Promise.resolve();
    });
    expect(result.current.a.cursor).toBe('c200');
  });

  it('keeps a refusal only this tab knows', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    const { result } = feed();
    await waitFor(() => expect(get).toHaveBeenCalled());
    updateBuffer('a', (b) => addCommands(b, [{ ...fromEntry(5), result: 'error', local: true }]));
    get.mockResolvedValue(answer([entry(100)], ''));
    await act(() => client.invalidateQueries());
    await waitFor(() => expect(result.current.a.cmds.map((c) => c.id)).toEqual(['r5', 'r100']));
  });

  it('does not replace a timeout by an older history entry', () => {
    const done = { ...fromEntry(1), result: 'timeout' as const };
    const late = { ...fromEntry(1), result: 'error' as const, reason: 'not_answered' };
    expect(addCommands({ ...emptyBuffer, cmds: [done] }, [late]).cmds[0]?.result).toBe('timeout');
  });

  it('knows a running command after a remount', async () => {
    vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    vi.spyOn(api, 'POST').mockImplementation(() => new Promise(() => undefined) as never);
    const first = feed();
    act(() => first.result.current.feed.send.mutate('stop'));
    await waitFor(() => expect(first.result.current.feed.send.isPending).toBe(true));
    first.unmount();
    const second = feed();
    expect(second.result.current.feed.send.isPending).toBe(true);
  });

  it('does not take a running command for failed once it is answered', () => {
    const done = {
      kind: 'cmd' as const,
      id: 'r',
      ts: 't',
      command: 'c',
      result: 'ok' as const,
      response: 'x',
    };
    const late = { ...done, result: 'error' as const, reason: 'not_answered', response: undefined };
    const merged = addCommands({ ...emptyBuffer, cmds: [done] }, [late]);
    expect(merged.cmds[0]).toMatchObject({ result: 'ok', response: 'x' });
    expect(merged.cmds[0]?.reason).toBeUndefined();
  });

  it('refills after a reconnect of the socket', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    feed();
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    await act(() => client.invalidateQueries());
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
  });

  it('fetches the newest commands when coming back', async () => {
    const get = vi.spyOn(api, 'GET').mockResolvedValue(page as never);
    const { rerender } = feed();
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    rerender({ id: 'b' });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    rerender({ id: 'a' });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(3));
  });
});
