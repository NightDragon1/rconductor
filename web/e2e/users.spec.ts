// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

const row = (page: Page, name: string) =>
  page.getByRole('row').filter({ has: page.getByRole('button', { name, exact: true }) });

async function openUsers(page: Page, state: StackState, user = 'admin') {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('admin/users');
  await expect(page.getByRole('heading', { name: 'Users', level: 1 })).toBeVisible();
}

const afterUndo = { timeout: 9000 };

test('search and filters (UI-007)', async ({ page, state }) => {
  await openUsers(page, state);
  const search = page.getByRole('searchbox', { name: 'Search users' });
  await search.fill('mod');
  await expect(row(page, 'mod')).toBeVisible();
  await expect(row(page, 'mod2')).toBeVisible();
  await expect(row(page, 'sup')).toHaveCount(0);
  await search.fill('');
  await page.getByLabel('Filter by status').selectOption('locked');
  await expect(row(page, 'locked')).toBeVisible();
  await expect(row(page, 'sup')).toHaveCount(0);
  await page.getByLabel('Filter by status').selectOption('invited');
  // The stack's expired invitation shows as such.
  await expect(
    page
      .getByRole('row')
      .filter({ hasText: '(invitation)' })
      .filter({ hasText: 'expired' })
      .first(),
  ).toBeVisible();
  await page.getByLabel('Filter by status').selectOption('all');
  await page.getByLabel('Filter by role').selectOption('server_admin');
  await expect(row(page, 'sadmin')).toBeVisible();
  await expect(row(page, 'mod')).toHaveCount(0);
});

test('a mass action reports each user, also a refusal (UI-007)', async ({ page, state }) => {
  await openUsers(page, state);
  await page.getByRole('checkbox', { name: 'Select sadmin', exact: true }).check();
  await page.getByRole('checkbox', { name: 'Select mod', exact: true }).check();
  const bar = page.getByRole('toolbar', { name: 'Mass actions' });
  await expect(bar).toContainText('2 selected');
  // A server scope fits only a server admin: the moderator gets 403.
  await bar.getByLabel('Assign server scope').selectOption({ label: 'survival' });
  const results = page.getByRole('dialog');
  await expect(results).toContainText('1 done, 1 failed.', afterUndo);
  const list = results.getByRole('list', { name: 'Result per user' });
  await expect(list.getByRole('listitem').filter({ hasText: 'sadmin' })).toContainText('done');
  await expect(list.getByRole('listitem').filter({ hasText: /^mod/ })).toContainText(
    'Not allowed for this user',
  );
  await expect(results).not.toContainText('Done for every user.');
  await results.getByRole('button', { name: 'Close' }).click();

  // Lock and unlock two users: both change.
  for (const name of ['mod2', 'sup'])
    await page.getByRole('checkbox', { name: `Select ${name}`, exact: true }).check();
  await bar.getByRole('button', { name: 'Lock', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Lock' }).click();
  await expect(page.getByRole('dialog')).toContainText('Done for every user.', afterUndo);
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  for (const name of ['mod2', 'sup']) await expect(row(page, name)).toContainText('locked');
  for (const name of ['mod2', 'sup'])
    await page.getByRole('checkbox', { name: `Select ${name}`, exact: true }).check();
  await bar.getByRole('button', { name: 'Unlock' }).click();
  await expect(page.getByRole('dialog')).toContainText('Done for every user.', afterUndo);
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  for (const name of ['mod2', 'sup']) await expect(row(page, name)).toContainText('active');
});

test('undo restores a change; shortcuts are off by default (UI-008)', async ({ page, state }) => {
  await openUsers(page, state);
  await row(page, 'mod2').getByRole('button', { name: 'mod2' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of mod2' });
  const release = panel.getByRole('checkbox', { name: /May manage supporters/ });
  const before = await release.isChecked();
  await release.setChecked(!before);
  await panel.getByRole('button', { name: 'Save' }).click();
  const note = page.getByRole('status').filter({ hasText: 'assignments of mod2' });
  await expect(note).toContainText('Undo brings back the rights, not the sessions');
  await expect(release).toBeChecked({ checked: !before });
  await note.getByRole('button', { name: 'Undo' }).click();
  // "Saved." still stands from the save; the box says when Undo is through.
  await expect(release).toBeChecked({ checked: before });
  await page.reload();
  await row(page, 'mod2').getByRole('button', { name: 'mod2' }).click();
  await expect(release).toBeChecked({ checked: before });

  // Shortcuts are off: "/" does not reach the search, "i" opens nothing.
  await page.locator('body').press('/');
  await expect(page.getByRole('searchbox', { name: 'Search users' })).not.toBeFocused();
  await page.locator('body').press('i');
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('a user from a template gets its assignments; a role change warns (UI-008)', async ({
  page,
  browser,
  state,
}) => {
  await openUsers(page, state);
  await row(page, 'mod').getByRole('button', { name: 'mod' }).click();
  await page
    .getByRole('complementary', { name: 'Details of mod' })
    .getByRole('button', { name: 'Use as template' })
    .click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Template')).toHaveValue(/.+/);
  await dialog.getByRole('button', { name: 'Create link' }).click();
  const url = (await dialog.locator('p.font-mono').textContent()) ?? '';
  expect(url).toMatch(/\/invite#/);
  await dialog.getByRole('button', { name: 'Close' }).click();

  const name = `tmpl-${Date.now() % 100000}`;
  const guest = await browser.newContext();
  const invitee = await guest.newPage();
  await invitee.goto(url);
  await invitee.getByLabel('Pseudonym').fill(name);
  await invitee.getByLabel('Password', { exact: true }).fill('a fine invited password');
  await invitee.getByLabel('Repeat the new password').fill('a fine invited password');
  await invitee.getByRole('button', { name: 'Create my account' }).click();
  await expect(invitee.getByRole('status')).toContainText('Your account is ready');
  await guest.close();

  await page.reload();
  // The template's moderator group came along.
  await expect(row(page, name)).toContainText('moderators');
  await row(page, name).getByRole('button', { name }).click();
  const panel = page.getByRole('complementary', { name: `Details of ${name}` });
  await panel.getByLabel('Role').selectOption('supporter');
  const warn = page.getByRole('dialog');
  await expect(warn).toContainText('Assignments of the old role will be removed; sessions end.');
  await warn.getByRole('button', { name: 'Change role' }).click();
  await expect(row(page, name)).toContainText('Supporter');
  await expect(row(page, name)).not.toContainText('moderators');
});

test('profile data only for those who manage the user (AUTH-009)', async ({
  page,
  browser,
  state,
}) => {
  const sup = await browser.newContext();
  const supPage = await sup.newPage();
  await login(supPage, 'sup', secret(state, 'sup'));
  await expect(supPage.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const status = await supPage.evaluate(() =>
    fetch('api/v1/me', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ real_name: 'Sam Supporter' }),
    }).then((r) => r.status),
  );
  expect(status).toBe(200);
  await sup.close();

  await openUsers(page, state, 'mod');
  await row(page, 'sup').getByRole('button', { name: 'sup' }).click();
  await expect(page.getByRole('complementary', { name: 'Details of sup' })).toContainText(
    'Sam Supporter',
  );
  // The server admin does not manage this supporter: not in his list.
  await page.context().clearCookies();
  await openUsers(page, state, 'sadmin');
  await expect(row(page, 'sup')).toHaveCount(0);
});

test('a role change ends the session at once, socket included (AUTH-006)', async ({
  page,
  browser,
  state,
}) => {
  await openUsers(page, state);
  const url = await page.evaluate(async () => {
    const r = await fetch('api/v1/invites', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role: 'moderator' }),
    });
    return ((await r.json()) as { url: string }).url;
  });
  const name = `role-${Date.now() % 100000}`;
  const password = 'a fine invited password';
  const second = await browser.newContext();
  const modPage = await second.newPage();
  await modPage.goto(url);
  await modPage.getByLabel('Pseudonym').fill(name);
  await modPage.getByLabel('Password', { exact: true }).fill(password);
  await modPage.getByLabel('Repeat the new password').fill(password);
  await modPage.getByRole('button', { name: 'Create my account' }).click();
  await expect(modPage.getByRole('status')).toContainText('Your account is ready');

  let closed = false;
  modPage.on('websocket', (ws) => ws.on('close', () => (closed = true)));
  await login(modPage, name, password);
  await expect(modPage.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();

  await page.reload();
  await row(page, name).getByRole('button', { name }).click();
  const panel = page.getByRole('complementary', { name: `Details of ${name}` });
  await panel.getByLabel('Role').selectOption('supporter');
  await page.getByRole('dialog').getByRole('button', { name: 'Change role' }).click();
  await expect(row(page, name)).toContainText('Supporter', afterUndo);

  await expect.poll(() => closed, { timeout: 5000 }).toBe(true);
  await modPage.reload();
  await expect(modPage.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await second.close();
});

test('locking ends the session at once, socket included (AUTH-006)', async ({
  page,
  browser,
  state,
}) => {
  const sup = await browser.newContext();
  const supPage = await sup.newPage();
  let closed = false;
  supPage.on('websocket', (ws) => ws.on('close', () => (closed = true)));
  await login(supPage, 'sup', secret(state, 'sup'));
  await expect(supPage.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();

  await openUsers(page, state);
  await row(page, 'sup').getByRole('button', { name: 'sup' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of sup' });
  await panel.getByRole('switch', { name: 'Locked' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Lock' }).click();
  await expect(row(page, 'sup')).toContainText('locked', afterUndo);
  await expect.poll(() => closed, { timeout: 5000 }).toBe(true);
  await supPage.reload();
  await expect(supPage.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await sup.close();

  await panel.getByRole('switch', { name: 'Locked' }).click();
  await expect(row(page, 'sup')).toContainText('active', afterUndo);
});
