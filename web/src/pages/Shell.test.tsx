// SPDX-License-Identifier: AGPL-3.0-or-later
import { act, screen } from '@testing-library/react';
import { useSyncExternalStore } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { bufferOwner } from '@/lib/console';
import { renderPage, setLanguage } from '@/test/render';
import { PanelVersion, Shell } from './Shell';

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
    return {
      data: { user: { id, pseudonym: id, role: 'moderator' }, ui: {}, panel_version: '0.10' },
    };
  },
}));

describe('Shell (Design 6.3)', () => {
  beforeEach(() => {
    live.opened = 0;
    live.closed = 0;
    user.id = 'alice';
    bufferOwner(undefined);
  });

  it('B-1: gives the next user a socket of their own', async () => {
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

  it('UI-015: shows the panel version to a moderator', async () => {
    await setLanguage('en');
    await renderPage(() => <Shell />);
    expect(screen.getByText('RCONductor 0.10')).toBeTruthy();
  });

  it('UI-015: a collapsed sidebar keeps the version short, the full text as title', async () => {
    await setLanguage('en');
    await renderPage(() => <PanelVersion collapsed />);
    const line = screen.getByText('0.10');
    expect(line.getAttribute('title')).toBe('RCONductor 0.10');
  });
});
