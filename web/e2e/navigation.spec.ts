// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';

// UI-011: the visible entries per role are exactly those of the table in
// Design 6.2 (topology since 1.1, UI-010; messages are not built yet). Entries
// without the right are hidden, not greyed.
const design62: Record<string, { main: string[]; admin: string[] }> = {
  // RCON admin: everything.
  admin: {
    main: ['Overview', 'Servers', 'Topology', 'Audit'],
    admin: ['Users', 'Groups', 'Command lists', 'Servers and groups', 'Agents', 'Settings'],
  },
  // Server admin: users (his moderators and supporters), moderator
  // groups, own lists, servers read-only in scope; no agents, no settings.
  sadmin: {
    main: ['Overview', 'Servers', 'Topology', 'Audit'],
    admin: ['Users', 'Groups', 'Command lists', 'Servers and groups'],
  },
  // Moderator with supporters on (global and for him): his supporters.
  mod: { main: ['Overview', 'Servers'], admin: ['Users'] },
  // Moderator without his own release: no administration.
  mod2: { main: ['Overview', 'Servers'], admin: [] },
  // Supporter: no administration.
  sup: { main: ['Overview', 'Servers'], admin: [] },
};

async function visibleEntries(page: Page) {
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  await expect(nav.getByRole('link').first()).toBeVisible();
  const lists = nav.getByRole('list');
  const main = await lists.nth(0).getByRole('link').allTextContents();
  const admin =
    (await lists.count()) > 1 ? await lists.nth(1).getByRole('link').allTextContents() : [];
  return { main, admin };
}

for (const [user, want] of Object.entries(design62)) {
  test(`navigation of ${user} matches Design 6.2`, async ({ page, state }) => {
    await login(page, user, secret(state, user));
    expect(await visibleEntries(page)).toEqual(want);
    // The profile is in the user block for every role.
    await expect(page.getByTitle('Your profile')).toBeVisible();
  });
}

// With the global supporter switch off, no moderator manages supporters,
// also one with his own release.
test('moderator without the global supporter switch', async ({ page, playwright, state }) => {
  const origin = new URL(state.base_url).origin;
  const admin = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: origin },
  });
  // Settings are versioned: the write names the ETag just read (revision
  // 2.4 candidate 24).
  const set = async (on: boolean) => {
    const etag = (await admin.get('api/v1/settings')).headers()['etag'] ?? '';
    const r = await admin.patch('api/v1/settings', {
      data: { 'supporters.enabled': on },
      headers: { 'If-Match': etag },
    });
    expect(r.status()).toBe(200);
  };
  expect(
    (
      await admin.post('api/v1/auth/login', {
        data: { pseudonym: 'admin', password: secret(state, 'admin') },
      })
    ).status(),
  ).toBe(200);
  await set(false);
  try {
    await login(page, 'mod', secret(state, 'mod'));
    expect(await visibleEntries(page)).toEqual({ main: ['Overview', 'Servers'], admin: [] });
    await page.goto('admin/users');
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  } finally {
    await set(true);
    await admin.dispose();
  }
});

test('pages outside the role do not exist', async ({ page, state }) => {
  await login(page, 'mod', secret(state, 'mod'));
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  for (const path of ['admin/agents', 'admin/settings', 'audit', 'admin/groups', 'topology']) {
    await page.goto(path);
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  }
  await page.goto('admin/users');
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible();
});

test('the sidebar collapses and stays collapsed', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  const width = () => nav.evaluate((el) => el.closest('aside')?.getBoundingClientRect().width ?? 0);
  expect(await width()).toBeGreaterThan(200);
  await page.getByRole('button', { name: 'Collapse' }).click();
  await expect.poll(width).toBeLessThan(80);
  await nav.getByRole('link', { name: 'Agents' }).click();
  await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Expand the navigation' })).toBeVisible();
});

test('on a phone the navigation is a menu', async ({ page, state }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeHidden();
  await page.getByRole('button', { name: 'Menu' }).click();
  const dialog = page.getByRole('dialog', { name: 'Menu' });
  await dialog.getByRole('link', { name: 'Servers', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('heading', { name: 'Servers', level: 1 })).toBeVisible();
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(390);
});

test('the design follows the system, light and dark can be chosen (UI-001)', async ({
  page,
  state,
}) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await login(page, 'admin', secret(state, 'admin'));
  const theme = () => page.evaluate(() => document.documentElement.dataset.theme);
  const button = page.getByRole('button', { name: /^Design:/ });
  await expect(button).toHaveAccessibleName(/like the system/);
  expect(await theme()).toBe('light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(theme).toBe('dark');
  await button.click();
  await expect(button).toHaveAccessibleName(/light/);
  expect(await theme()).toBe('light');
  await page.reload();
  expect(await theme()).toBe('light');
  await button.click();
  expect(await theme()).toBe('dark');
  await page.emulateMedia({ colorScheme: 'light' });
  expect(await theme()).toBe('dark');
});

test('O7: the design switch shows its state by icon, tooltip and name (UI-001)', async ({
  page,
  state,
}) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const button = page.getByRole('button', { name: /^Design:/ });
  const shown = async (design: string, name: string, applied: string) => {
    await expect(button).toHaveAccessibleName(`Design: ${name}. Change it`);
    await expect(button).toHaveAttribute('title', `Design: ${name}. Change it`);
    await expect(button.locator('svg')).toHaveCount(1);
    await expect(button.locator('svg')).toHaveAttribute('data-design', design);
    await expect
      .poll(() => page.evaluate(() => document.documentElement.dataset.theme))
      .toBe(applied);
  };
  // The cycle keeps its order auto, light, dark; each step survives a reload.
  const cycle: [string, string, string][] = [
    ['light', 'light', 'light'],
    ['dark', 'dark', 'dark'],
    ['auto', 'auto, like the system', 'dark'],
  ];
  await shown('auto', 'auto, like the system', 'dark');
  const icons = new Set([await button.locator('svg').getAttribute('class')]);
  for (const [design, name, applied] of cycle) {
    await button.click();
    await shown(design, name, applied);
    icons.add(await button.locator('svg').getAttribute('class'));
    expect(await page.evaluate(() => localStorage.getItem('rcd.design'))).toBe(design);
    await page.reload();
    await shown(design, name, applied);
  }
  // Each state has its own icon.
  expect(icons.size).toBe(3);
  // Auto follows the system again.
  await page.emulateMedia({ colorScheme: 'light' });
  await shown('auto', 'auto, like the system', 'light');
});

test('a whole card opens its server and shows the pointer (UI-004, UI-001)', async ({
  page,
  state,
}) => {
  await login(page, 'admin', secret(state, 'admin'));
  const card = page.getByRole('article', { name: 'survival', exact: true });
  const before = await card.evaluate((e) => getComputedStyle(e).backgroundColor);
  await card.hover({ position: { x: 20, y: 60 } });
  await expect
    .poll(() => card.evaluate((e) => getComputedStyle(e).backgroundColor))
    .not.toBe(before);
  await card.click({ position: { x: 20, y: 60 } });
  await expect(page).toHaveURL(new RegExp(`servers/${state.servers.survival}`));
});

test('a password can be shown at sign-in and hides for sending (UI-013)', async ({
  page,
  state,
}) => {
  await page.goto('login');
  const field = page.getByLabel('Password', { exact: true });
  await field.fill('some password');
  await page.getByRole('button', { name: 'Show password' }).click();
  await expect(field).toHaveAttribute('type', 'text');
  await page.getByLabel('Pseudonym').fill('admin');
  await field.fill(secret(state, 'admin'));
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
});

test('explanations in the settings can be hidden (UI-014)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('admin/settings');
  const help = page.getByText('Audit entries older than this are removed.');
  await expect(help).toBeVisible();
  await page.getByLabel('Show explanations').uncheck();
  await expect(help).toHaveCount(0);
  await page.reload();
  await expect(page.getByLabel('Show explanations')).not.toBeChecked();
  await expect(help).toHaveCount(0);
});
