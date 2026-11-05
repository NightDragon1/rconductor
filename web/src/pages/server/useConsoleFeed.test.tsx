// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { resetBuffers, useBuffer } from '@/lib/console';
import { useConsoleFeed } from './useConsoleFeed';

vi.mock('@/api/session', () => ({ useSession: () => ({ data: undefined }) }));

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

const page = { data: { items: [], next_cursor: '' }, response: new Response() };

function feed() {
  return renderHook(
    ({ id }) => ({ feed: useConsoleFeed(id, false), a: useBuffer('a'), b: useBuffer('b') }),
    { wrapper, initialProps: { id: 'a' } },
  );
}

describe('useConsoleFeed (CMD-001)', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    resetBuffers();
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
