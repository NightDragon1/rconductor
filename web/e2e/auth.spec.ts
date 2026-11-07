// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

test('the RCON admin signs in and out', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await expect(page.getByTitle('Your profile')).toContainText('admin');
  // UI-015: the sidebar names the panel version.
  await expect(page.getByText(/^RCONductor \d+\.\d+/)).toBeVisible();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  // Without a session every page leads to the sign-in.
  await page.goto('./');
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
});

test('signing out closes the live socket at once (AUTH-006)', async ({ page, state }) => {
  let closed = false;
  page.on('websocket', (ws) => ws.on('close', () => (closed = true)));
  await login(page, 'sadmin', secret(state, 'sadmin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  expect(closed).toBe(false);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await expect.poll(() => closed, { timeout: 5000 }).toBe(true);
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

test('a session that ended leads to the sign-in at the next request (#161)', async ({
  page,
  state,
}) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.context().clearCookies();
  await page.getByRole('link', { name: 'Audit', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
});
