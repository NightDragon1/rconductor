// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen } from '@testing-library/react';
import { Bot } from 'lucide-react';
import { afterAll, describe, expect, it, vi } from 'vitest';
import { mainNav, type NavEntry } from '@/nav';
import { renderPage, setLanguage } from '@/test/render';
import { Shell } from './Shell';

vi.mock('@/lib/live', () => ({
  LiveSocket: class {
    close() {}
    subscribe() {
      return () => undefined;
    }
  },
}));

const flags = vi.hoisted(() => ({ on: [] as string[] }));
vi.mock('@/api/session', async (original) => ({
  ...(await original<typeof import('@/api/session')>()),
  useSession: () => ({
    data: {
      user: { id: 'boss', pseudonym: 'boss', role: 'rcon_admin' },
      ui: {
        features: flags.on,
        shortcuts_enabled: false,
        supporters_enabled: false,
        supporters_allowed: false,
      },
      panel_version: '0.10',
    },
  }),
}));

const entry: NavEntry = {
  to: '/flagged',
  key: 'flagged',
  icon: Bot,
  allowed: () => true,
  feature: 'test_flag',
};
mainNav.push(entry);
afterAll(() => {
  mainNav.splice(mainNav.indexOf(entry), 1);
});

describe('feature flags in the navigation (NFR-019)', () => {
  it('shows the entry only while its flag is on', async () => {
    await setLanguage('en');
    for (const [on, want] of [
      [[], false],
      [['test_flag'], true],
    ] as const) {
      flags.on = [...on];
      const { unmount } = await renderPage(() => <Shell />);
      const links = screen.getAllByRole('link').map((l) => l.getAttribute('href'));
      expect(links.includes('/flagged')).toBe(want);
      unmount();
    }
  });
});
