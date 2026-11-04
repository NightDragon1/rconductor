// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';

// UI-011: the visible entries per role are exactly those of the table in
// Design 6.2 (R1 rows; topology and messages are R2). Entries without the
// right are hidden, not greyed.
const design62: Record<string, { main: string[]; admin: string[] }> = {
  // RCON admin: everything.
  admin: {
    main: ['Overview', 'Servers', 'Audit'],
    admin: ['Users', 'Groups', 'Command lists', 'Servers and groups', 'Agents', 'Settings'],
  },
  // Server admin: users (his moderators and supporters), moderator
  // groups, own lists, servers read-only in scope; no agents, no settings.
  sadmin: {
    main: ['Overview', 'Servers', 'Audit'],
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
  for (const path of ['admin/agents', 'admin/settings', 'audit', 'admin/groups']) {
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

test('the design switches between dark and light', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  const theme = () => page.evaluate(() => document.documentElement.dataset.theme);
  expect(await theme()).toBe('dark');
  await page.getByRole('button', { name: 'Switch between dark and light design' }).click();
  expect(await theme()).toBe('light');
  await page.reload();
  expect(await theme()).toBe('light');
});
