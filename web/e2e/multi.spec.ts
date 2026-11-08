// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// CMD-010 (D-069): one command to several servers from the console, a
// result per server; a server that fails leaves the others.
test('a command to several servers answers per server (CMD-010)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/console`);
  await page.getByRole('button', { name: 'Several servers' }).click();
  const dialog = page.getByRole('dialog', { name: 'Send to several servers' });
  // The current server is picked already.
  await expect(dialog.getByRole('checkbox', { name: 'survival' })).toBeChecked();
  await dialog.getByRole('checkbox', { name: 'creative' }).check();
  await dialog.getByRole('checkbox', { name: 'arena' }).check();
  await dialog.getByLabel('Command').fill('say to all');
  await dialog.getByRole('button', { name: 'Send to 3 servers' }).click();
  const results = dialog.getByRole('region', { name: 'Results' });
  await expect(results.getByRole('listitem', { name: 'survival' })).toContainText('OK');
  await expect(results.getByRole('listitem', { name: 'creative' })).toContainText('OK');
  await expect(results.getByRole('listitem', { name: 'arena' })).toContainText('error');
});
