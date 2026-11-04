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

// shoot grows the window to the page unless the page holds a box sized by
// the window (the console); then it takes the whole page as it is.
async function shoot(page: Page, name: string, grow = true) {
  mkdirSync(dir, { recursive: true });
  if (!grow) {
    for (const theme of ['dark', 'light']) {
      await page.evaluate((t) => {
        document.documentElement.dataset.theme = t;
        localStorage.setItem('rcd.theme', t);
      }, theme);
      await page.waitForTimeout(400);
      await page.screenshot({ path: `${dir}/${name}-${theme}.png`, fullPage: true });
    }
    return;
  }
  // The window grows to the page, so the sticky sidebar reaches down too.
  const width = page.viewportSize()?.width ?? 1440;
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  await page.setViewportSize({ width, height: Math.max(height, 844) });
  await page.evaluate(() => window.scrollTo(0, 0));
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

// The console with history, a fresh answer, logs and open suggestions
// (step 5).
for (const [width, name] of [
  [1440, 'console-admin'],
  [390, 'phone-console-admin'],
] as const) {
  test(`screenshot ${name}`, async ({ page, state }) => {
    await page.setViewportSize({ width, height: width < 500 ? 844 : 900 });
    await login(page, 'admin', secret(state, 'admin'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(`servers/${state.servers.survival}/console`);
    const input = page.getByRole('textbox', { name: 'Command' });
    await input.fill('list');
    await input.press('Enter');
    await expect(page.getByRole('article', { name: 'list', exact: true }).last()).toBeVisible();
    await input.fill('whitelist');
    await page.waitForTimeout(500);
    await shoot(page, name, false);
  });
}

// The user administration (step 8a): list, selection with mass actions and
// the side panel; the moderator's view; the phone.
for (const [user, width, name] of [
  ['admin', 1440, 'users-admin'],
  ['mod', 1440, 'users-moderator'],
  ['admin', 390, 'phone-users-admin'],
] as const) {
  test(`screenshot ${name}`, async ({ page, state }) => {
    await page.setViewportSize({ width, height: 900 });
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto('admin/users');
    await expect(page.getByRole('heading', { name: 'Users', level: 1 })).toBeVisible();
    await page.waitForLoadState('networkidle');
    if (width < 500) {
      // No selection on a phone: it reads (UI-012).
      await page.getByRole('button', { name: 'mod', exact: true }).click();
    } else if (user === 'admin') {
      await page.getByRole('checkbox', { name: 'Select mod', exact: true }).check();
      await page.getByRole('checkbox', { name: 'Select mod2', exact: true }).check();
      await page.getByRole('button', { name: 'mod', exact: true }).click();
    } else {
      await page.getByRole('button', { name: 'sup', exact: true }).click();
    }
    await page.waitForTimeout(300);
    await shoot(page, name);
  });
}

// The administration (step 8): every page, some with the side panel open.
for (const [user, path, open, name] of [
  ['admin', 'admin/groups', 'moderators', 'groups-admin'],
  ['admin', 'admin/command-lists', 'moderation', 'command-lists-admin'],
  ['admin', 'admin/servers', 'survival', 'servers-admin'],
  ['sadmin', 'admin/servers', 'survival', 'servers-server-admin'],
  ['admin', 'admin/agents', '', 'agents-admin'],
  ['admin', 'admin/settings', '', 'settings-admin'],
  ['admin', 'audit', '', 'audit-admin'],
  ['mod', 'profile', '', 'profile-moderator'],
] as const) {
  test(`screenshot ${name}`, async ({ page, state }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    if (open) await page.getByRole('button', { name: open, exact: true }).first().click();
    await page.waitForTimeout(300);
    await shoot(page, name);
  });
}

test('screenshot phone-settings-admin', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('admin/settings');
  await page.waitForLoadState('networkidle');
  await shoot(page, 'phone-settings-admin');
});

// The phone view (step 9): head with actions, commands with the sheet, and
// the administration reading.
test('screenshot phone server actions', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/overview`);
  await page.waitForLoadState('networkidle');
  await page.getByRole('button', { name: 'Server actions' }).click();
  await shoot(page, 'phone-server-actions', false);
});

test('screenshot phone commands', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'mod', secret(state, 'mod'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/commands`);
  await page.waitForLoadState('networkidle');
  await shoot(page, 'phone-commands-moderator', false);
});

for (const [path, name] of [
  ['admin/groups', 'phone-groups'],
  ['admin/command-lists', 'phone-command-lists'],
  ['admin/servers', 'phone-servers-admin'],
  ['admin/agents', 'phone-agents'],
  ['admin/settings', 'phone-settings'],
] as const) {
  test(`screenshot ${name}`, async ({ page, state }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await login(page, 'admin', secret(state, 'admin'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await shoot(page, name);
  });
}
