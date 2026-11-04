// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

test('the RCON admin signs in and out', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await expect(page.getByTitle('Your profile')).toContainText('admin');
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  // Without a session every page leads to the sign-in.
  await page.goto('./');
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
});

test('a wrong password is refused', async ({ page }) => {
  await login(page, 'admin', 'not the password');
  await expect(page.getByRole('alert')).toHaveText('Pseudonym or password is wrong.');
});

// AUTH-010: a temporary password allows only the change of the password.
test('a temporary password forces a new one', async ({ page, state }) => {
  await login(page, state.reset_user, state.reset_password);
  await expect(page.getByRole('heading', { name: 'Choose a new password' })).toBeVisible();
  await page.goto('./');
  await expect(page.getByRole('heading', { name: 'Choose a new password' })).toBeVisible();
  await page.getByLabel('Current password').fill(state.reset_password);
  await page.getByLabel('New password', { exact: true }).fill('a brand new e2e password');
  await page.getByLabel('Repeat the new password').fill('a brand new e2e password');
  await page.getByRole('button', { name: 'Change password' }).click();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
});
