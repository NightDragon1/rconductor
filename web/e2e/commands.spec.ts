// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

const cards = (page: Page) => page.getByRole('region', { name: 'Commands' }).getByRole('form');
const card = (page: Page, path: string) =>
  page.getByRole('region', { name: 'Commands' }).getByRole('form', { name: path, exact: true });
const mine = (page: Page) => page.getByRole('region', { name: 'My history' });

async function open(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}`);
  await expect(page).toHaveURL(/\/commands$/);
}

test('the moderator sees his list as cards with fields (CMD-003)', async ({ page, state, csp }) => {
  void csp;
  await open(page, state, 'mod');
  await expect(cards(page)).toHaveCount(6);
  for (const path of ['list', 'kick', 'whitelist add', 'say', 'time set', 'weather']) {
    await expect(card(page, path)).toBeVisible();
  }
  const kick = card(page, 'kick');
  await expect(kick).toContainText('kick {player} {reason}');
  await expect(kick.getByLabel('player', { exact: true })).toBeVisible();
  await expect(kick.getByLabel('reason (optional)')).toBeVisible();
  // choice is a switch with the profile's values.
  const time = card(page, 'time set');
  await expect(time.getByRole('radio')).toHaveText(['day', 'night']);
  await expect(time.getByRole('button', { name: 'Run' })).toBeDisabled();
  // A command without catalog entry gets a free field (Design 7.3).
  await expect(card(page, 'weather').getByLabel('Arguments')).toBeVisible();
});

test('players come from the player list; the answer shows in the history (CMD-003)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await open(page, state, 'mod');
  const players = page.getByRole('region', { name: 'Players online' });
  // Names are foreign data and stay text (SEC-006).
  await expect(players.getByRole('button')).toHaveText(['Steve', '<b>Alex</b>']);
  const kick = card(page, 'kick');
  await kick.getByLabel('player', { exact: true }).click();
  await players.getByRole('button', { name: 'Steve' }).click();
  await expect(kick.getByLabel('player', { exact: true })).toHaveValue('Steve');
  await kick.getByLabel('reason (optional)').fill('spam in chat');
  await expect(kick).toContainText('› kick Steve spam in chat');
  await kick.getByRole('button', { name: 'Run' }).click();
  const entry = mine(page).getByRole('listitem').filter({ hasText: 'kick Steve spam in chat' });
  await expect(entry).toContainText('OK');
  await expect(entry).toContainText('Done: kick Steve spam in chat');

  const time = card(page, 'time set');
  await time.getByRole('radio', { name: 'night' }).click();
  await time.getByRole('button', { name: 'Run' }).click();
  await expect(mine(page).getByRole('listitem').first()).toContainText('time set night');

  const weather = card(page, 'weather');
  await weather.getByLabel('Arguments').fill('clear 600');
  await weather.getByRole('button', { name: 'Run' }).click();
  await expect(mine(page).getByRole('listitem').first()).toContainText('weather clear 600');

  await mine(page).getByRole('link', { name: 'Show all' }).click();
  await expect(page).toHaveURL(/\/history$/);
  await expect(page.getByRole('region', { name: 'History' })).toContainText('weather clear 600');
});

test('a player field takes only player names (revision 2.4 candidate 19)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await open(page, state, 'mod');
  const wl = card(page, 'whitelist add');
  const field = wl.getByLabel('player', { exact: true });
  await field.fill('two words');
  await expect(wl).toContainText('One word, no spaces');
  await expect(wl.getByRole('button', { name: 'Run' })).toBeDisabled();
  for (const selector of ['@a', '@e[type=player]', '"x"']) {
    await field.fill(selector);
    await expect(wl).toContainText('Not a player name');
    await expect(wl.getByRole('button', { name: 'Run' })).toBeDisabled();
  }
  // A name outside the profile's player_pattern cannot be picked.
  await field.click();
  const players = page.getByRole('region', { name: 'Players online' });
  await expect(players.getByRole('button', { name: '<b>Alex</b>' })).toBeDisabled();
  await expect(players.getByRole('button', { name: 'Steve' })).toBeEnabled();
});

test('the supporter sees only passable commands (CMD-003, ROLE-007)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await open(page, state, 'sup');
  await expect(cards(page)).toHaveCount(4);
  for (const path of ['list', 'whitelist add', 'say', 'time set']) {
    await expect(card(page, path)).toBeVisible();
  }
  await expect(card(page, 'kick')).toHaveCount(0);
  await expect(card(page, 'weather')).toHaveCount(0);
  await card(page, 'list').getByRole('button', { name: 'Run' }).click();
  await expect(mine(page).getByRole('listitem').first()).toContainText(
    'There are 2 of a max of 20',
  );
});

test('the search narrows the cards by path or description', async ({ page, state, csp }) => {
  void csp;
  await open(page, state, 'mod');
  const search = page.getByRole('searchbox', { name: 'Search commands' });
  await search.fill('WHITE');
  await expect(cards(page)).toHaveCount(1);
  await expect(card(page, 'whitelist add')).toBeVisible();
  await search.fill('kick a player');
  await expect(cards(page)).toHaveCount(1);
  await search.fill('nothing like this');
  await expect(cards(page)).toHaveCount(0);
  await expect(page.getByText('No command matches.')).toBeVisible();
  await search.fill('');
  await expect(cards(page)).toHaveCount(6);
});
