// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// UI-011 with Design 6.2: each role sees only its entries; the others are
// hidden, and their pages do not exist for it.
const everyEntry = [
  'Overview',
  'Servers',
  'Audit',
  'Users',
  'Groups',
  'Command lists',
  'Servers and groups',
  'Agents',
  'Settings',
];
const expected: Record<string, string[]> = {
  admin: everyEntry,
  sadmin: [
    'Overview',
    'Servers',
    'Audit',
    'Users',
    'Groups',
    'Command lists',
    'Servers and groups',
  ],
  mod: ['Overview', 'Servers', 'Users'],
  sup: ['Overview', 'Servers'],
};

for (const [user, entries] of Object.entries(expected)) {
  test(`navigation of ${user}`, async ({ page, state, csp }) => {
    void csp;
    await login(page, user, secret(state, user));
    const nav = page.getByRole('navigation', { name: 'Main navigation' });
    await expect(nav.getByRole('link').first()).toBeVisible();
    await expect(nav.getByRole('link')).toHaveText(entries);
  });
}

test('pages outside the role do not exist', async ({ page, state, csp }) => {
  void csp;
  await login(page, 'mod', secret(state, 'mod'));
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  for (const path of ['admin/agents', 'admin/settings', 'audit', 'admin/groups']) {
    await page.goto(path);
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  }
  await page.goto('admin/users');
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible();
});

test('the sidebar collapses and stays collapsed', async ({ page, state, csp }) => {
  void csp;
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

test('on a phone the navigation is a menu', async ({ page, state, csp }) => {
  void csp;
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeHidden();
  await page.getByRole('button', { name: 'Menu' }).click();
  const dialog = page.getByRole('dialog', { name: 'Menu' });
  await dialog.getByRole('link', { name: 'Servers', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('heading', { name: 'Servers' })).toBeVisible();
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(390);
});

test('the design switches between dark and light', async ({ page, state, csp }) => {
  void csp;
  await login(page, 'admin', secret(state, 'admin'));
  const theme = () => page.evaluate(() => document.documentElement.dataset.theme);
  expect(await theme()).toBe('dark');
  await page.getByRole('button', { name: 'Switch between dark and light design' }).click();
  expect(await theme()).toBe('light');
  await page.reload();
  expect(await theme()).toBe('light');
});
