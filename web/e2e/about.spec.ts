// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// The About dialog in tabs (UI-024): the program, the third-party
// components of the build with license and copyright, and the agents for
// the RCON admin only.

test('the RCON admin sees the program, the components and the agents', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await page.getByRole('button', { name: 'About' }).click();
  const dialog = page.getByRole('dialog', { name: 'About RCONductor' });
  await expect(dialog.getByText('© 2026 NightDragon')).toBeVisible();
  await expect(dialog.getByText('ShadowGhost.NET')).toBeVisible();
  const third = dialog.getByRole('tab', { name: /^Third-party \(\d+\)$/ });
  await expect(third).toBeVisible();
  const count = Number(/\((\d+)\)/.exec((await third.textContent()) ?? '')?.[1]);
  expect(count).toBeGreaterThan(0);
  await third.click();
  const items = dialog.getByRole('list', { name: 'Third-party licenses' }).getByRole('listitem');
  await expect(items).toHaveCount(count);
  await expect(items.filter({ hasText: /^react \d/ })).toContainText('MIT');
  await dialog.getByRole('tab', { name: 'Agents' }).click();
  await expect(dialog.getByRole('region', { name: 'Agents' })).toBeVisible();
});

test('a moderator gets no agents tab', async ({ page, state }) => {
  await login(page, 'mod', secret(state, 'mod'));
  await page.getByRole('button', { name: 'About' }).click();
  const dialog = page.getByRole('dialog', { name: 'About RCONductor' });
  await expect(dialog.getByRole('tab', { name: 'About' })).toBeVisible();
  await expect(dialog.getByRole('tab', { name: /^Third-party/ })).toBeVisible();
  await expect(dialog.getByRole('tab', { name: 'Agents' })).toHaveCount(0);
});
