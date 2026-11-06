// SPDX-License-Identifier: AGPL-3.0-or-later
import { act } from '@testing-library/react';
import { useSyncExternalStore } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { bufferOwner } from '@/lib/console';
import { renderPage, setLanguage } from '@/test/render';
import { Shell } from './Shell';

const live = vi.hoisted(() => ({ opened: 0, closed: 0 }));
vi.mock('@/lib/live', () => ({
  LiveSocket: class {
    constructor() {
      live.opened += 1;
    }
    close() {
      live.closed += 1;
    }
    subscribe() {
      return () => undefined;
    }
  },
}));

const user = vi.hoisted(() => ({ id: 'alice', listeners: new Set<() => void>() }));
vi.mock('@/api/session', async (original) => ({
  ...(await original<typeof import('@/api/session')>()),
  useSession: () => {
    const id = useSyncExternalStore(
      (l) => {
        user.listeners.add(l);
        return () => user.listeners.delete(l);
      },
      () => user.id,
    );
    return { data: { user: { id, pseudonym: id, role: 'moderator' }, ui: {} } };
  },
}));

describe('Shell (Design 6.3)', () => {
  beforeEach(() => {
    live.opened = 0;
    live.closed = 0;
    user.id = 'alice';
    bufferOwner(undefined);
  });

  it('gives the next user a socket of their own', async () => {
    await setLanguage('en');
    await renderPage(() => <Shell />);
    const { opened, closed } = live;
    expect(opened).toBeGreaterThan(0);

    act(() => {
      user.id = 'bob';
      for (const l of user.listeners) l();
    });
    expect(live.closed).toBeGreaterThan(closed);
    expect(live.opened).toBeGreaterThan(opened);
  });
});
