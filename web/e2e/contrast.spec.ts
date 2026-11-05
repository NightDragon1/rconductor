// SPDX-License-Identifier: AGPL-3.0-or-later
import { AxeBuilder } from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';

// UI-001: every page is readable in both designs, text at 4.5:1 or more.
// axe measures what the browser really drew; the colors of symbols and
// frames are checked per design in src/lib/contrast.test.ts.
const server = ['', '/console', '/commands', '/logs', '/history'];
const pages: Record<string, string[]> = {
  admin: [
    '',
    'servers',
    'audit',
    'profile',
    'admin/users',
    'admin/groups',
    'admin/command-lists',
    'admin/servers',
    'admin/agents',
    'admin/settings',
  ],
  sadmin: ['', 'admin/users', 'admin/groups', 'admin/command-lists', 'admin/servers'],
  mod: ['', 'servers', 'admin/users'],
  sup: ['', 'servers'],
};

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
      for (const tab of server) {
        await page.goto(`servers/${state.server_id}${tab}`);
        await check(page, theme, `server ${tab}`);
      }
    });
  });
}
