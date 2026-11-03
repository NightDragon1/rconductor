// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Changes wait five seconds for Undo (UI-008).
const saved = { timeout: 9000 };

async function as(page: Page, state: StackState, user: string, path: string, title: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(path);
  await expect(page.getByRole('heading', { name: title, level: 1 })).toBeVisible();
}

test('a list keeps one command locked for supporters (ROLE-007)', async ({ page, state, csp }) => {
  void csp;
  await as(page, state, 'admin', 'admin/command-lists', 'Command lists');
  await page.getByPlaceholder('Name of a new list').fill('event-cmds');
  await page.getByRole('button', { name: 'Create list' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of event-cmds' });
  for (const path of ['say', 'list']) {
    await panel.getByLabel('Add a command path').fill(path);
    await panel.getByRole('button', { name: 'Add' }).click();
  }
  await panel.getByLabel('say passable to supporters').check();
  await expect(panel.getByRole('status')).toHaveText('Saved.', saved);
  // All passable: the page refuses before the backend would.
  await panel.getByLabel('list passable to supporters').check();
  await expect(panel.getByRole('alert')).toContainText('At least one command must stay locked');
  await page.reload();
  const row = page.getByRole('row').filter({ hasText: 'event-cmds' });
  await expect(row).toContainText('list, say');
  await expect(row.getByRole('cell').nth(2)).toHaveText('1');
  // The backend refuses it too.
  const id = await page.evaluate(async () => {
    const r = await fetch('api/v1/command-lists').then((x) => x.json());
    return (r.items as { id: string; name: string }[]).find((l) => l.name === 'event-cmds')?.id;
  });
  const status = await page.evaluate(
    (listId) =>
      fetch(`api/v1/command-lists/${listId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          entries: [
            { path: 'say', passable: true },
            { path: 'list', passable: true },
          ],
        }),
      }).then((r) => r.status),
    id,
  );
  expect(status).toBe(422);
});

test('a moderator group gives its members servers and commands (ROLE-004, ROLE-005)', async ({
  page,
  browser,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'admin', 'admin/groups', 'Groups');
  await page.getByPlaceholder('Name of a new group').fill('event-mods');
  await page.getByRole('button', { name: 'Create group' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of event-mods' });
  await panel.getByLabel('Command list').selectOption({ label: 'event-cmds' });
  await panel.getByLabel('+ Member').selectOption({ label: 'mod2' });
  await panel.getByLabel('+ Server or group').selectOption({ label: 'arena' });
  await expect(panel.getByRole('status')).toHaveText('Saved.', saved);
  await expect(page.getByRole('row').filter({ hasText: 'event-mods' })).toContainText('event-cmds');

  // mod2 now has arena with the commands of the list.
  const other = await browser.newContext();
  const mod2 = await other.newPage();
  await login(mod2, 'mod2', secret(state, 'mod2'));
  await expect(mod2.getByRole('article', { name: 'arena', exact: true })).toBeVisible();
  await mod2.goto(`servers/${state.servers.arena}/commands`);
  const cards = mod2.getByRole('region', { name: 'Commands' }).getByRole('form');
  await expect(cards).toHaveCount(2);
  await expect(cards.filter({ hasText: 'Message to all players' })).toBeVisible();
  await other.close();

  // Deleting asks first, then takes the server away.
  await panel.getByRole('button', { name: 'Delete group …' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete group …' }).click();
  await expect(page.getByRole('row').filter({ hasText: 'event-mods' })).toHaveCount(0);
  const again = await browser.newContext();
  const mod2b = await again.newPage();
  await login(mod2b, 'mod2', secret(state, 'mod2'));
  await expect(mod2b.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await expect(mod2b.getByRole('article', { name: 'arena', exact: true })).toHaveCount(0);
  await again.close();
});

test('a server admin sees only his own groups and lists', async ({ page, state, csp }) => {
  void csp;
  await as(page, state, 'sadmin', 'admin/groups', 'Groups');
  await expect(page.getByRole('row').filter({ hasText: 'moderators' })).toHaveCount(0);
  await page.goto('admin/command-lists');
  await expect(page.getByRole('row').filter({ hasText: 'moderation' })).toHaveCount(0);
  await expect(page.getByText('No command lists.')).toBeVisible();
});

test('a stale draft is not sent; reload shows the other change (revision 2.4 candidate 24)', async ({
  page,
  browser,
  state,
  csp,
}) => {
  void csp;
  await as(page, state, 'admin', 'admin/command-lists', 'Command lists');
  await page.getByPlaceholder('Name of a new list').fill('lock-test');
  await page.getByRole('button', { name: 'Create list' }).click();
  const mine = page.getByRole('complementary', { name: 'Details of lock-test' });
  await expect(mine).toBeVisible();

  // A second session of the admin opens the same list.
  const other = await browser.newContext();
  const page2 = await other.newPage();
  await as(page2, state, 'admin', 'admin/command-lists', 'Command lists');
  await page2.getByRole('button', { name: 'lock-test' }).click();
  const theirs = page2.getByRole('complementary', { name: 'Details of lock-test' });

  await theirs.getByLabel('Add a command path').fill('say');
  await theirs.getByRole('button', { name: 'Add' }).click();
  await expect(theirs.getByRole('status')).toHaveText('Saved.', saved);

  await mine.getByLabel('Add a command path').fill('list');
  await mine.getByRole('button', { name: 'Add' }).click();
  await expect(mine.getByRole('alert')).toHaveText('Changed by someone else – reload', saved);
  const entries = await page.evaluate(async () => {
    const r = await fetch('api/v1/command-lists').then((x) => x.json());
    return (r.items as { name: string; entries: { path: string }[] }[])
      .find((l) => l.name === 'lock-test')
      ?.entries.map((e) => e.path);
  });
  expect(entries).toEqual(['say']);
  await mine.getByRole('button', { name: 'Reload' }).click();
  await expect(mine.getByRole('cell').filter({ hasText: /^say$/ })).toBeVisible();
  await expect(mine.getByRole('cell').filter({ hasText: /^list$/ })).toHaveCount(0);
  await other.close();
});
