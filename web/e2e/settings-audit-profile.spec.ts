// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

const saved = { timeout: 9000 };

async function as(page: Page, state: StackState, user: string, path: string, title: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(path);
  await expect(page.getByRole('heading', { name: title, level: 1 })).toBeVisible();
}

test('settings: a change waits for undo and saves with the ETag (Design 5.6)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'admin', 'admin/settings', 'Settings');
  const field = page.getByLabel('invite.lifetime');
  const before = await field.inputValue();
  await field.fill('48 h');
  await page.getByRole('button', { name: 'Save 1 change' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible(saved);
  await page.reload();
  await expect(page.getByLabel('invite.lifetime')).toHaveValue('48 h');
  await page.getByLabel('invite.lifetime').fill(before);
  await page.getByRole('button', { name: 'Save 1 change' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible(saved);
});

test('switching supporters off asks first', async ({ page, state, csp }) => {
  void csp;
  await as(page, state, 'admin', 'admin/settings', 'Settings');
  await page.getByLabel('supporters.enabled').uncheck();
  await page.getByRole('button', { name: 'Save 1 change' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('All supporters lose their access');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'Discard' }).click();
  await expect(page.getByLabel('supporters.enabled')).toBeChecked();
});

test('the RCON admin switches shortcuts on for everyone (UI-008)', async ({ page, state, csp }) => {
  void csp;
  await as(page, state, 'admin', 'admin/settings', 'Settings');
  await page.getByLabel('ui.shortcuts_enabled').check();
  await page.getByRole('button', { name: 'Save 1 change' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible(saved);
  try {
    await page.goto('admin/users');
    await expect(page.getByRole('heading', { name: 'Users', level: 1 })).toBeVisible();
    await page.locator('body').press('/');
    await expect(page.getByRole('searchbox', { name: 'Search users' })).toBeFocused();
  } finally {
    await page.goto('admin/settings');
    await page.getByLabel('ui.shortcuts_enabled').uncheck();
    await page.getByRole('button', { name: 'Save 1 change' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible(saved);
  }
});

test('audit with filters; the server admin sees his scope (AUD-001)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'admin', 'audit', 'Audit');
  const entries = page.getByRole('list', { name: 'Audit entries' }).getByRole('listitem');
  await expect(entries.first()).toBeVisible();
  await page.getByLabel('Kind').selectOption('command');
  await expect(entries.first()).toContainText('Command');
  await expect(entries.filter({ hasText: 'Administration' })).toHaveCount(0);
  await page.context().clearCookies();
  await as(page, state, 'sadmin', 'audit', 'Audit');
  await expect(page.getByRole('list', { name: 'Audit entries' })).toBeVisible();
  // The admin's sign-ins are not in the server admin's scope.
  await page.getByLabel('Kind').selectOption('auth');
  await expect(entries.filter({ hasText: 'admin' }).filter({ hasText: 'RCON admin' })).toHaveCount(
    0,
  );
});

test('own profile: real name, contacts, picture; a wrong password is refused (AUTH-009)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await login(page, 'mod2', secret(state, 'mod2'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.getByTitle('Your profile').click();
  await expect(page.getByRole('heading', { name: 'Your profile', level: 1 })).toBeVisible();
  const about = page.getByRole('form', { name: 'About you' });
  await about.getByLabel('Real name').fill('Morgan Mod');
  await about.getByRole('button', { name: '+ Contact' }).click();
  await about.getByLabel('Kind of contact').fill('telegram');
  await about.getByLabel('Contact', { exact: true }).fill('@morgan');
  await about.getByRole('button', { name: 'Save' }).click();
  await expect(about.getByRole('status')).toHaveText('Saved.');
  await page.reload();
  await expect(page.getByRole('form', { name: 'About you' }).getByLabel('Real name')).toHaveValue(
    'Morgan Mod',
  );

  const png = Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
    'base64',
  );
  const picture = page.getByRole('region', { name: 'Profile picture' });
  await picture
    .getByLabel('Choose a picture (PNG, JPEG or WebP)')
    .setInputFiles({ name: 'me.png', mimeType: 'image/png', buffer: png });
  await expect(picture.getByRole('status')).toHaveText('Picture saved.');
  await expect(picture.getByRole('img', { name: 'Your profile picture' })).toBeVisible();
  await picture.getByRole('button', { name: 'Remove picture' }).click();
  await expect(picture.getByRole('status')).toHaveText('Picture removed.');

  const pw = page.getByRole('form', { name: 'Change password' });
  await pw.getByLabel('Current password').fill('not my password at all');
  await pw.getByLabel('New password', { exact: true }).fill('another long password');
  await pw.getByLabel('Repeat the new password').fill('another long password');
  await pw.getByRole('button', { name: 'Change password' }).click();
  await expect(pw.getByRole('alert')).toHaveText(
    'The password was not changed. Check the current password.',
  );
});
