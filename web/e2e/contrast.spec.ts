// SPDX-License-Identifier: AGPL-3.0-or-later
import { AxeBuilder } from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';
import { pages, serverTabs } from './pages';

// UI-001: every page is readable in both designs, text at 4.5:1 or more.
// axe measures what the browser really drew; the colors of symbols and
// frames are checked per design in src/lib/contrast.test.ts.
async function design(page: Page, theme: string) {
  await page.addInitScript((t) => localStorage.setItem('rcd.theme', t), theme);
}

async function check(page: Page, theme: string, what: string) {
  await page.waitForLoadState('networkidle').catch(() => undefined);
  expect(await page.evaluate(() => document.documentElement.dataset.theme ?? 'dark')).toBe(theme);
  const { violations } = await new AxeBuilder({ page }).withRules(['color-contrast']).analyze();
  const found = violations.flatMap((v) =>
    v.nodes.map((n) => `${n.target.join(' ')}: ${n.any[0]?.message ?? n.failureSummary}`),
  );
  expect.soft(found, what).toEqual([]);
  // UI-001: nothing on a page can be dragged around.
  await expect.soft(page.locator('[draggable="true"]'), what).toHaveCount(0);
}

// The focus ring of a card's link against the card, with the opacity of a
// stopped card, in the color the browser really draws.
async function ringContrast(page: Page, name: string) {
  const link = page
    .getByRole('article', { name, exact: true })
    .getByRole('link', { name: new RegExp(name) })
    .first();
  await page.keyboard.press('Tab');
  await link.focus();
  return link.evaluate((el) => {
    const rgb = (c: string) => (c.match(/[\d.]+/g) ?? []).slice(0, 3).map(Number);
    const lum = (c: number[]) => {
      const [r = 0, g = 0, b = 0] = c.map((v) => {
        const x = v / 255;
        return x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const card = el.closest('article') as HTMLElement;
    const style = getComputedStyle(el);
    const alpha = Number(getComputedStyle(card).opacity);
    const bg = rgb(getComputedStyle(card).backgroundColor);
    const ring = rgb(style.outlineColor).map((v, i) => alpha * v + (1 - alpha) * (bg[i] ?? 0));
    const [hi, lo] = [lum(ring), lum(bg)].sort((a, b) => b - a) as [number, number];
    return { width: style.outlineWidth, ratio: (hi + 0.05) / (lo + 0.05) };
  });
}

for (const theme of ['dark', 'light']) {
  test.describe(`${theme} design`, () => {
    test.beforeEach(async ({ page }) => design(page, theme));

    test('the pages without a session', async ({ page }) => {
      await page.goto('login');
      await check(page, theme, 'login');
    });

    for (const [user, paths] of Object.entries(pages)) {
      test(`the pages of ${user}`, async ({ page, state }) => {
        await login(page, user, secret(state, user));
        await expect(page.getByRole('navigation').first()).toBeVisible();
        for (const path of paths) {
          await page.goto(path || './');
          await check(page, theme, `${user} /${path}`);
        }
      });
    }

    // Card frames stay below 3:1: they only structure the page. A card is
    // known by its title and status, and its focus ring reaches 3:1.
    test('a card is recognisable without its frame', async ({ page, state }) => {
      await login(page, 'admin', secret(state, 'admin'));
      await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
      await expect(page.getByRole('article')).not.toHaveCount(0);
      for (const card of await page.getByRole('article').all()) {
        await expect(card.getByRole('heading', { level: 3 })).toBeVisible();
        await expect(card.locator('span.shrink-0').first()).not.toBeEmpty();
      }
      for (const name of ['survival', 'lobby']) {
        const ring = await ringContrast(page, name);
        expect(ring.width, name).toBe('2px');
        expect(ring.ratio, name).toBeGreaterThanOrEqual(3);
      }
    });

    test('the dialogs and menus', async ({ page, state }) => {
      await login(page, 'admin', secret(state, 'admin'));
      await expect(page.getByRole('navigation').first()).toBeVisible();
      await page.goto(`servers/${state.server_id}`);
      await page
        .getByRole('group', { name: 'Server actions' })
        .getByRole('button', { name: 'Stop' })
        .click();
      await expect(page.getByRole('dialog')).toBeVisible();
      await check(page, theme, 'confirm dialog');
      await page.keyboard.press('Escape');

      await page.setViewportSize({ width: 390, height: 844 });
      await page.getByRole('button', { name: 'Server actions' }).click();
      await expect(page.getByRole('menu')).toBeVisible();
      await check(page, theme, 'actions menu');
      await page.keyboard.press('Escape');
      await page.getByRole('button', { name: 'Menu' }).click();
      await expect(page.getByRole('dialog', { name: 'Menu' })).toBeVisible();
      await check(page, theme, 'phone menu');
    });

    test('the pages of a server', async ({ page, state }) => {
      await login(page, 'admin', secret(state, 'admin'));
      await expect(page.getByRole('navigation').first()).toBeVisible();
      for (const tab of serverTabs) {
        await page.goto(`servers/${state.server_id}${tab}`);
        await check(page, theme, `server ${tab}`);
      }
    });
  });
}
