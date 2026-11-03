// SPDX-License-Identifier: AGPL-3.0-or-later
import { mkdirSync } from 'node:fs';
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';

// Screenshots for the review after the steps of M4 (dark, light, 390 px),
// written next to the mockups: SCREENSHOTS=<step> npx playwright test
// screenshots --project root.
const step = process.env.SCREENSHOTS;
const dir = `../docs/screenshots/m4-${step}`;

test.skip(!step, 'only with SCREENSHOTS=<step>');

// settled waits for the live player counts on an overview.
async function settled(page: Page) {
  const survival = page.getByRole('article', { name: 'survival', exact: true });
  if (await survival.count()) await expect(survival).toContainText('Players');
}

async function shoot(page: Page, name: string) {
  mkdirSync(dir, { recursive: true });
  // The window grows to the page, so the sticky sidebar reaches down too.
  const width = page.viewportSize()?.width ?? 1440;
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  await page.setViewportSize({ width, height: Math.max(height, 844) });
  for (const theme of ['dark', 'light']) {
    await page.evaluate((t) => {
      document.documentElement.dataset.theme = t;
      localStorage.setItem('rcd.theme', t);
    }, theme);
    // Colours fade over 150 ms after a switch.
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${dir}/${name}-${theme}.png` });
  }
}

const pages: [string, string, string][] = [
  ['admin', './', 'overview-admin'],
  ['admin', 'admin/agents', 'agents-admin'],
  ['sadmin', './', 'overview-server-admin'],
  ['mod', './', 'overview-moderator'],
  ['mod2', './', 'overview-moderator-without-release'],
  ['sup', './', 'overview-supporter'],
];

for (const [user, path, name] of pages) {
  test(`screenshot ${name}`, async ({ page, state }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page, user, secret(state, user));
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await settled(page);
    await shoot(page, name);
  });
}

test('screenshot phone', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'admin', secret(state, 'admin'));
  await page.waitForLoadState('networkidle');
  await settled(page);
  await shoot(page, 'phone-overview');
  await page.getByRole('button', { name: 'Menu' }).click();
  await shoot(page, 'phone-menu');
});

test('screenshot phone moderator', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'mod', secret(state, 'mod'));
  await page.waitForLoadState('networkidle');
  await settled(page);
  await shoot(page, 'phone-overview-moderator');
});
