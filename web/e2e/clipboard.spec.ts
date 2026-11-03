// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// Over http to a name that is not localhost, as to a LAN address (D-011),
// the page is no secure context and has no clipboard API: copying selects
// the text and asks for Ctrl+C (revision 2.4 candidate 17).
test('copying without a secure context selects the text', async ({ page, state, csp }) => {
  void csp;
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  expect(await page.evaluate(() => window.isSecureContext)).toBe(false);
  expect(new URL(page.url()).hostname).toBe('rcon.lan');

  // Console block.
  await page.goto(`servers/${state.servers.survival}/console`);
  const b = page.getByRole('article', { name: 'say hi from mod' });
  await b.getByRole('button', { name: 'Copy the block' }).click();
  await expect(b.getByRole('status')).toHaveText('Selected. Press Ctrl+C to copy.');
  expect(await page.evaluate(() => window.getSelection()?.toString())).toBe(
    'Done: say hi from mod',
  );

  // History entry.
  await page.goto(`servers/${state.servers.survival}/history`);
  const entry = page
    .getByRole('region', { name: 'History' })
    .getByRole('listitem')
    .filter({ hasText: 'say hi from mod' });
  await entry.getByText('Answer', { exact: true }).click();
  await entry.getByRole('button', { name: 'Copy the answer' }).click();
  await expect(entry.getByRole('status')).toHaveText('Selected. Press Ctrl+C to copy.');
  expect(await page.evaluate(() => window.getSelection()?.toString())).toBe(
    'Done: say hi from mod',
  );
});
