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

test('a server with profile, host, port, password and an agent container (SRV-001)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  await page.getByRole('button', { name: 'New server' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of New server' });
  await panel.getByLabel('Name').fill('e2e-new');
  await panel.getByLabel('Game profile').selectOption({ label: 'Minecraft (Java Edition)' });
  await panel.getByLabel('Host').fill('mc.example');
  await panel.getByLabel('RCON port').fill('25575');
  await panel.getByLabel('RCON password').fill('a secret rcon password');
  // Agent and container are optional and chosen from the agent's list.
  await panel.getByLabel('Agent').selectOption({ label: 'host-1' });
  await expect(panel.getByLabel('Container').locator('option')).toContainText([
    'mc-lobby · exited',
  ]);
  await panel.getByLabel('Container').selectOption('mc-lobby');
  await panel.getByRole('button', { name: 'Create server' }).click();
  const row = page.getByRole('row').filter({ hasText: 'e2e-new' });
  await expect(row).toContainText('mc.example:25575');
  await expect(row).toContainText('mc-lobby');
  // The password is never shown again.
  const opened = page.getByRole('complementary', { name: 'Details of e2e-new' });
  await expect(opened.getByLabel('New RCON password')).toHaveValue('');

  await opened.getByRole('button', { name: 'Delete server …' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete server …' }).click();
  await expect(row).toHaveCount(0);
});

test('a server can be in several server groups (SRV-003)', async ({ page, state, csp }) => {
  void csp;
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  await page.getByPlaceholder('Name of a new server group').fill('e2e-group');
  await page.getByRole('button', { name: 'Create group' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of e2e-group' });
  await panel.getByLabel('+ Server').selectOption({ label: 'survival' });
  await panel.getByLabel('+ Server').selectOption({ label: 'arena' });
  await expect(panel.getByRole('status')).toHaveText('Saved.', saved);
  const survival = page
    .getByRole('row')
    .filter({ has: page.getByRole('button', { name: 'survival', exact: true }) });
  await expect(survival).toContainText('minecraft');
  await expect(survival).toContainText('e2e-group');

  await panel.getByRole('button', { name: 'Delete group …' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete group …' }).click();
  await expect(survival).not.toContainText('e2e-group');
});

test('the server admin reads servers and groups only (Design 6.2)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'sadmin', 'admin/servers', 'Servers and groups');
  await expect(page.getByRole('button', { name: 'New server' })).toHaveCount(0);
  await page.getByRole('button', { name: 'survival', exact: true }).click();
  const panel = page.getByRole('complementary', { name: 'Details of survival' });
  await expect(panel.getByLabel('Name')).toBeDisabled();
  await expect(panel.getByRole('button', { name: 'Save' })).toHaveCount(0);
  await expect(panel.getByLabel('RCON password')).toHaveCount(0);
});

test('pairing dialog with token and Compose; approval by fingerprint (AGT-005)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'admin', 'admin/agents', 'Agents');
  // Pairing by fingerprint is off by default.
  await expect(page.getByLabel('Pairing by fingerprint')).not.toBeChecked();
  await page.getByRole('button', { name: 'Pair an agent' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Make token' }).click();
  const compose = dialog.getByLabel('Compose service', { exact: true });
  await expect(compose).toContainText('RCD_PAIRING_TOKEN');
  await expect(compose).toContainText(/rcd1\./);
  await dialog.getByRole('button', { name: 'Close' }).click();

  // A pairing that waits since the stack started is approved here.
  const waiting = page.getByRole('row').filter({ hasText: 'host-3' });
  await expect(waiting).toContainText('waiting for approval');
  await waiting.getByRole('button', { name: 'Approve' }).click();
  await expect(page.getByRole('status')).toHaveText('host-3 approved.');
  await expect(waiting).not.toContainText('waiting for approval');
});

test('the RCON admin reaches every administration page (ROLE-002)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  for (const [path, title] of [
    ['admin/users', 'Users'],
    ['admin/groups', 'Groups'],
    ['admin/command-lists', 'Command lists'],
    ['admin/servers', 'Servers and groups'],
    ['admin/agents', 'Agents'],
    ['audit', 'Audit'],
  ]) {
    await page.goto(path);
    await expect(page.getByRole('heading', { name: title, level: 1 })).toBeVisible();
  }
  for (const id of Object.values(state.servers)) {
    await page.goto(`servers/${id}/overview`);
    await expect(page.getByRole('heading', { name: 'Page not found' })).toHaveCount(0);
  }
});
