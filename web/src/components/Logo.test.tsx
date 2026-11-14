// SPDX-License-Identifier: AGPL-3.0-or-later
/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { LoginPage } from '@/pages/Login';
import { Shell } from '@/pages/Shell';
import { renderPage, setLanguage } from '@/test/render';

vi.mock('@/lib/live', () => ({
  LiveSocket: class {
    close() {}
    subscribe() {
      return () => undefined;
    }
  },
}));
vi.mock('@/api/session', async (original) => ({
  ...(await original<typeof import('@/api/session')>()),
  useSession: () => ({
    data: {
      user: { id: 'alice', pseudonym: 'alice', role: 'moderator' },
      ui: {},
      panel_version: '1.2',
    },
  }),
}));

// The rule of index.css that picks the variant of the design; jsdom does
// not read the Tailwind file itself.
const rule = /\/\* The logo in the variant[\s\S]*?\*\/\s*([^{]+\{[^}]*\})/.exec(
  readFileSync('src/index.css', 'utf8'),
)?.[1];

// wordMarks finds both variants, the hidden one too: a hidden image has
// no accessible name, so the alt text is matched as an attribute.
function wordMarks() {
  return [...document.querySelectorAll<HTMLImageElement>('img[alt="ShadowGhost.NET RCONductor"]')];
}

// marks finds both variants of the mark alone, the hidden one too.
function marks() {
  return [...document.querySelectorAll<HTMLImageElement>('img[alt="RCONductor"]')];
}

function visible(img: HTMLElement) {
  return getComputedStyle(img).display !== 'none';
}

describe('project logo (UI-023)', () => {
  beforeAll(async () => {
    await setLanguage('en');
    const style = document.createElement('style');
    style.textContent = rule ?? '';
    document.head.append(style);
  });
  afterEach(() => {
    delete document.documentElement.dataset.theme;
    localStorage.clear();
  });

  it('index.css has the rule for the variants', () => {
    expect(rule).toMatch(/\.logo-dark/);
    expect(rule).toMatch(/\.logo-light/);
  });

  it('shows the word mark in the sidebar, the variant of each design', async () => {
    await renderPage(() => <Shell />);
    const [light, dark] = wordMarks() as [HTMLImageElement, HTMLImageElement];
    expect(light.getAttribute('src')).toMatch(/logo\.svg$/);
    expect(dark.getAttribute('src')).toMatch(/logo-dark\.svg$/);
    for (const [theme, shown, hidden] of [
      ['light', light, dark],
      ['dark', dark, light],
    ] as const) {
      document.documentElement.dataset.theme = theme;
      expect(visible(shown), `${theme}: own variant`).toBe(true);
      expect(visible(hidden), `${theme}: other variant`).toBe(false);
    }
    // Before the design is applied, the dark base shows the dark variant.
    delete document.documentElement.dataset.theme;
    expect(visible(dark)).toBe(true);
    expect(visible(light)).toBe(false);
  });

  it('shows the mark alone in the collapsed sidebar, the variant of each design', async () => {
    localStorage.setItem('rcd.nav.collapsed', '1');
    await renderPage(() => <Shell />);
    expect(wordMarks()).toHaveLength(0);
    const [light, dark] = marks() as [HTMLImageElement, HTMLImageElement];
    expect(marks()).toHaveLength(2);
    expect(light.getAttribute('src')).toMatch(/mark-light\.svg$/);
    expect(dark.getAttribute('src')).toMatch(/mark\.svg$/);
    expect(dark.getAttribute('src')).not.toMatch(/mark-light\.svg$/);
    for (const [theme, shown, hidden] of [
      ['light', light, dark],
      ['dark', dark, light],
    ] as const) {
      document.documentElement.dataset.theme = theme;
      expect(visible(shown), `${theme}: own variant`).toBe(true);
      expect(visible(hidden), `${theme}: other variant`).toBe(false);
      // Only the variant of the design is in the accessibility tree.
      expect(screen.getByRole('img', { name: 'RCONductor' })).toBe(shown);
    }
    delete document.documentElement.dataset.theme;
    expect(visible(dark)).toBe(true);
    expect(visible(light)).toBe(false);
  });

  it('shows the word mark on the login page', async () => {
    await renderPage(() => <LoginPage />);
    expect(screen.getByRole('heading', { name: 'Sign in' })).toBeTruthy();
    const logos = wordMarks();
    expect(logos.map((l) => l.getAttribute('src')?.replace(/^.*\//, ''))).toEqual([
      'logo.svg',
      'logo-dark.svg',
    ]);
    // Only one of them is in the accessibility tree.
    expect(screen.getAllByRole('img', { name: 'ShadowGhost.NET RCONductor' })).toHaveLength(1);
  });
});
