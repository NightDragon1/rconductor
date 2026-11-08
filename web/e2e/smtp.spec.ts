// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// NFR-017 in the interface: the RCON admin saves the SMTP setup and sends a
// test mail; the result names only the category. The stack has no SMTP
// server, so the mail goes to a closed port.
test('SMTP setup and test mail (NFR-017)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('admin/settings');
  const panel = page.getByRole('region', { name: 'Mail (SMTP)' });
  await panel.getByLabel('SMTP host').fill('127.0.0.1');
  await panel.getByLabel('Port').fill('1');
  await panel.getByLabel('Encryption').selectOption('none');
  await panel.getByLabel('Sender address').fill('panel@example.org');
  await panel.getByRole('button', { name: 'Save' }).click();
  await expect(panel.getByRole('status')).toHaveText('Saved.');

  await panel.getByRole('button', { name: 'Send a test mail' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Recipient').fill('someone@example.com');
  await dialog.getByRole('button', { name: 'Send' }).click();
  await expect(dialog.getByRole('status')).toHaveText(
    'Not sent: the SMTP server could not be reached.',
  );
});

test('a server admin has no SMTP setup (NFR-017)', async ({ page, state }) => {
  await login(page, 'sadmin', secret(state, 'sadmin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const r = await page.request.get('api/v1/settings/smtp');
  expect(r.status()).toBe(403);
});
