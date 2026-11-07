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
}) => {
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
  // The dialog closes after Create; the password is never shown again.
  const opened = page.getByRole('complementary', { name: 'Details of e2e-new' });
  await expect(panel).toHaveCount(0);
  await row.getByRole('button', { name: 'e2e-new' }).click();
  await expect(opened.getByLabel('New RCON password')).toHaveValue('');

  await opened.getByRole('button', { name: 'Delete server …' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete server …' }).click();
  await expect(row).toHaveCount(0);
});

test('servers, server groups and agents can be searched (UI-007)', async ({ page, state }) => {
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  const server = (name: string) => page.getByRole('button', { name, exact: true });
  const search = page.getByRole('searchbox', { name: 'Search servers and server groups' });
  await search.fill('SURV');
  await expect(server('survival')).toBeVisible();
  await expect(server('arena')).toHaveCount(0);
  await expect(server('minecraft')).toHaveCount(0);
  await search.fill('minecraft');
  await expect(server('minecraft')).toBeVisible();
  await expect(server('survival')).toHaveCount(0);
  await search.fill('');
  await expect(server('arena')).toBeVisible();

  await page.goto('admin/agents');
  const agents = page.getByRole('searchbox', { name: 'Search agents' });
  await agents.fill('host-1');
  await expect(page.getByRole('row').filter({ hasText: 'host-1' })).toBeVisible();
  await expect(page.getByRole('row').filter({ hasText: 'host-2' })).toHaveCount(0);
  await agents.fill('');
  await expect(page.getByRole('row').filter({ hasText: 'host-2' })).toBeVisible();
});

test('a server needs no agent (SRV-001)', async ({ page, state }) => {
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  await page.getByRole('button', { name: 'New server' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of New server' });
  await panel.getByLabel('Name').fill('e2e-plain');
  await panel.getByLabel('Game profile').selectOption({ label: 'Minecraft (Java Edition)' });
  await panel.getByLabel('Host').fill('mc.example');
  await panel.getByLabel('RCON port').fill('25575');
  await panel.getByLabel('RCON password').fill('a secret rcon password');
  await panel.getByRole('button', { name: 'Create server' }).click();
  const row = page.getByRole('row').filter({ hasText: 'e2e-plain' });
  await expect(row).toContainText('mc.example:25575');

  await row.getByRole('button', { name: 'e2e-plain' }).click();
  const opened = page.getByRole('complementary', { name: 'Details of e2e-plain' });
  // Undo takes a change back, but not a new password (D-048); host and
  // port keep the stored password and can be undone (D-050).
  await opened.getByLabel('Name').fill('e2e-plain2');
  await opened.getByRole('button', { name: 'Save' }).click();
  const note = page.getByRole('status').filter({ hasText: 'Saved: Server e2e-plain2 changed' });
  await note.getByRole('button', { name: 'Undo' }).click();
  await expect(opened.getByLabel('Name')).toHaveValue('e2e-plain');
  await opened.getByLabel('New RCON password').fill('another rcon password');
  await opened.getByRole('button', { name: 'Save' }).click();
  const plain = page.getByRole('status').filter({ hasText: 'Saved: Server e2e-plain changed' });
  await expect(plain.last()).toBeVisible();
  await expect(plain.last().getByRole('button', { name: 'Undo' })).toHaveCount(0);
  await opened.getByLabel('Host').fill('mc2.example');
  await opened.getByRole('button', { name: 'Save' }).click();
  await expect(row).toContainText('mc2.example:25575');
  await plain.getByRole('button', { name: 'Undo' }).click();
  await expect(opened.getByLabel('Host')).toHaveValue('mc.example');
  await expect(row).toContainText('mc.example:25575');
  await opened.getByRole('button', { name: 'Delete server …' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete server …' }).click();
  await expect(row).toHaveCount(0);
});

test('a server can be in several server groups (SRV-003)', async ({ page, state }) => {
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  await page.getByPlaceholder('Name of a new server group').fill('e2e-group');
  await page.getByRole('button', { name: 'Create group' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of e2e-group' });
  await panel.getByLabel('+ Server').selectOption({ label: 'survival' });
  await panel.getByLabel('+ Server').selectOption({ label: 'arena' });
  await panel.getByRole('button', { name: 'Save' }).click();
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

test('the server admin reads servers and groups only (Design 6.2)', async ({ page, state }) => {
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
}) => {
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

test('the RCON admin reaches every administration page (ROLE-002)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  for (const [path, title] of [
    ['admin/users', 'Users'],
    ['admin/groups', 'Groups'],
    ['admin/command-lists', 'Command lists'],
    ['admin/servers', 'Servers and groups'],
    ['admin/agents', 'Agents'],
    ['admin/settings', 'Settings'],
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

test('test connection reports the state without saving (SRV-007)', async ({ page, state }) => {
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  await page.getByRole('button', { name: 'New server' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of New server' });
  await panel.getByLabel('Game profile').selectOption({ label: 'Minecraft (Java Edition)' });
  await panel.getByLabel('Host').fill('127.0.0.1');
  await panel.getByLabel('RCON port').fill('1');
  await panel.getByLabel('RCON password').fill('some password');
  await panel.getByRole('button', { name: 'Test connection' }).click();
  await expect(panel.getByText('cannot be reached')).toBeVisible();
  await expect(page.getByRole('row').filter({ hasText: '127.0.0.1:1' })).toHaveCount(0);

  // The running connection answers for unchanged data.
  await panel.getByRole('button', { name: 'Close the details' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Discard changes' }).click();
  await page.getByRole('button', { name: 'survival', exact: true }).click();
  const open = page.getByRole('complementary', { name: 'Details of survival' });
  await open.getByRole('button', { name: 'Test connection' }).click();
  await expect(open.getByText('Connection works')).toBeVisible();
});

test('a refused input shows the reason of the backend (UI-016)', async ({ page, state }) => {
  await as(page, state, 'admin', 'admin/servers', 'Servers and groups');
  for (const name of ['e2e-twice', 'e2e-twice']) {
    await page.getByRole('button', { name: 'New server' }).click();
    const panel = page.getByRole('complementary', { name: 'Details of New server' });
    await panel.getByLabel('Name').fill(name);
    await panel.getByLabel('Game profile').selectOption({ label: 'Minecraft (Java Edition)' });
    await panel.getByLabel('Host').fill('twice.example');
    await panel.getByLabel('RCON port').fill('25575');
    await panel.getByLabel('RCON password').fill('a secret rcon password');
    await panel.getByRole('button', { name: 'Create server' }).click();
  }
  const panel = page.getByRole('complementary', { name: 'Details of New server' });
  await expect(panel.getByText('a server with this name exists')).toBeVisible();
  await expect(panel.getByText('Not possible this way')).toHaveCount(0);
  await panel.getByRole('button', { name: 'Close the details' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Discard changes' }).click();
  const row = page.getByRole('row').filter({ hasText: 'e2e-twice' });
  await row.getByRole('button', { name: 'e2e-twice' }).click();
  const opened = page.getByRole('complementary', { name: 'Details of e2e-twice' });
  await opened.getByRole('button', { name: 'Delete server …' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete server …' }).click();
  await expect(row).toHaveCount(0);
});
