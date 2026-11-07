// SPDX-License-Identifier: AGPL-3.0-or-later
import { AxeBuilder } from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';
import { pages, serverTabs } from './pages';

// UI-001: every page is readable in both designs, text at 4.5:1 or more.
// axe measures what the browser really drew; the colors of symbols and
// frames are checked per design in src/lib/contrast.test.ts.
async function design(page: Page, theme: string) {
  await page.addInitScript((t) => localStorage.setItem('rcd.design', t), theme);
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

// weakFrames lists controls whose border stands below 3:1 against the
// background behind them.
async function weakFrames(page: Page) {
  return page.evaluate(() => {
    const rgb = (c: string) => (c.match(/[\d.]+/g) ?? []).map(Number);
    const lum = (c: number[]) => {
      const [r = 0, g = 0, b = 0] = c.map((v) => {
        const x = v / 255;
        return x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const behind = (el: Element | null): number[] => {
      for (let e = el; e; e = e.parentElement) {
        const c = rgb(getComputedStyle(e).backgroundColor);
        if (c.length === 3 || (c[3] ?? 0) > 0) return c;
      }
      return [255, 255, 255];
    };
    const out: string[] = [];
    const controls = 'a, button, input, select, textarea, [role="radio"]';
    for (const el of Array.from(document.querySelectorAll<HTMLElement>(controls))) {
      const s = getComputedStyle(el);
      if (parseFloat(s.borderTopWidth) === 0 || s.borderTopStyle === 'none') continue;
      if (el.closest('[disabled]') || (el as HTMLInputElement).disabled) continue;
      const [hi, lo] = [lum(rgb(s.borderTopColor)), lum(behind(el.parentElement))].sort(
        (a, b) => b - a,
      ) as [number, number];
      const ratio = (hi + 0.05) / (lo + 0.05);
      if (ratio < 3)
        out.push(`${el.tagName} ${(el.textContent ?? '').slice(0, 24)}: ${ratio.toFixed(2)}`);
    }
    return out;
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

    // The frame of each control the browser really draws, not only the
    // token: 3:1 against what is behind it (WCAG 1.4.11).
    test('controls have a visible frame', async ({ page, state }) => {
      await login(page, 'admin', secret(state, 'admin'));
      await expect(page.getByRole('navigation').first()).toBeVisible();
      for (const path of [
        `servers/${state.server_id}/console`,
        'audit',
        'admin/users',
        'admin/servers',
      ]) {
        await page.goto(path);
        await page.waitForLoadState('networkidle').catch(() => undefined);
        expect.soft(await weakFrames(page), path).toEqual([]);
      }
      await login(page, 'mod', secret(state, 'mod'));
      await expect(page.getByRole('navigation').first()).toBeVisible();
      await page.goto(`servers/${state.server_id}/commands`);
      await page.waitForLoadState('networkidle').catch(() => undefined);
      expect.soft(await weakFrames(page), 'commands').toEqual([]);
    });

    test('an open row keeps its text readable', async ({ page, state }) => {
      await login(page, 'admin', secret(state, 'admin'));
      await expect(page.getByRole('navigation').first()).toBeVisible();
      await page.goto('admin/users');
      await page.getByRole('button', { name: 'mod', exact: true }).click();
      await check(page, theme, 'open user row');
      await page.goto('admin/servers');
      await page.getByRole('button', { name: 'arena', exact: true }).click();
      await check(page, theme, 'open server row');
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
