// SPDX-License-Identifier: AGPL-3.0-or-later
import { mkdirSync } from 'node:fs';
import type { Page } from '@playwright/test';
import { login, secret, test } from './fixtures';

// Screenshots for the review after the steps of M4 (dark, light, 390 px),
// written next to the mockups: SCREENSHOTS=<step> npx playwright test
// screenshots --project root.
const step = process.env.SCREENSHOTS;
const dir = `../docs/screenshots/m4-${step}`;

test.skip(!step, 'only with SCREENSHOTS=<step>');

async function shoot(page: Page, name: string) {
  mkdirSync(dir, { recursive: true });
  for (const theme of ['dark', 'light']) {
    await page.evaluate((t) => {
      document.documentElement.dataset.theme = t;
      localStorage.setItem('rcd.theme', t);
    }, theme);
    // Colours fade over 150 ms after a switch.
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${dir}/${name}-${theme}.png`, fullPage: true });
  }
}

const pages: [string, string, string][] = [
  ['admin', './', 'overview-admin'],
  ['admin', 'admin/agents', 'agents-admin'],
  ['mod', './', 'overview-moderator'],
];

for (const [user, path, name] of pages) {
  test(`screenshot ${name}`, async ({ page, state }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page, user, secret(state, user));
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await shoot(page, name);
  });
}

test('screenshot phone', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'admin', secret(state, 'admin'));
  await page.waitForLoadState('networkidle');
  await shoot(page, 'phone-overview');
  await page.getByRole('button', { name: 'Menu' }).click();
  await shoot(page, 'phone-menu');
});
