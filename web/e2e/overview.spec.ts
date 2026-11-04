// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';

// The stack has survival (running, sha256 image), creative (restart
// loop), lobby (stopped) and arena (container running, RCON password
// rejected) on agent host-1, outpost without agent, a disconnected agent
// host-2 and a locked user (internal/e2e/stack).
const card = (page: Page, name: string) => page.getByRole('article', { name, exact: true });

test('every role lands on its overview (UI-002)', async ({ page, state }) => {
  const landing: Record<string, { health: boolean; cards: string[]; load: boolean }> = {
    admin: {
      health: true,
      cards: ['survival', 'creative', 'lobby', 'arena', 'outpost', 'hostile'],
      load: true,
    },
    sadmin: { health: false, cards: ['survival'], load: true },
    mod: { health: false, cards: ['survival'], load: false },
    sup: { health: false, cards: ['survival'], load: false },
  };
  for (const [user, want] of Object.entries(landing)) {
    await page.context().clearCookies();
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await expect(page.getByRole('article')).toHaveCount(want.cards.length);
    for (const name of want.cards) await expect(card(page, name)).toBeVisible();
    await expect(page.getByRole('region', { name: 'Notices' })).toHaveCount(want.health ? 1 : 0);
    await expect(card(page, 'survival').getByRole('meter')).toHaveCount(want.load ? 2 : 0);
  }
});

test('the health overview names every problem with a link (UI-003)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  const tile = (name: string) => page.getByRole('region', { name, exact: true });
  await expect(tile('Servers')).toContainText('3 / 6');
  await expect(tile('Servers')).toContainText('1 stopped');
  await expect(tile('Agents')).toContainText('1 / 2');
  await expect(tile('RCON errors')).toContainText('1');
  await expect(tile('RCON errors')).toContainText('arena');
  await expect(tile('Restart loops')).toContainText('creative');
  await expect(tile('Users')).toContainText('1 locked');

  const notices = page.getByRole('region', { name: 'Notices' }).getByRole('listitem');
  const row = (name: string) => notices.filter({ hasText: name });
  await expect(notices).toHaveCount(4);
  await expect(row('creative').getByRole('link')).toHaveAttribute(
    'href',
    new RegExp(`servers/${state.servers.creative}$`),
  );
  await expect(row('arena')).toContainText('RCON password rejected · container running');
  await expect(row('host-2')).toContainText('agent disconnected');
  await expect(row('host-2').getByRole('link')).toHaveAttribute('href', /admin\/agents#/);
  await expect(row('locked').getByRole('link')).toHaveAttribute(
    'href',
    new RegExp(`admin/users#${state.locked_user_id}$`),
  );
  // The jump leads to the object.
  await row('arena').getByRole('link', { name: 'Open server' }).click();
  await expect(page).toHaveURL(new RegExp(`servers/${state.servers.arena}(/console)?$`));
});

test('cards show only fields with a source (UI-004)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  const survival = card(page, 'survival');
  // A bare sha256 image shows the profile instead.
  await expect(survival).toContainText('Profile: Minecraft (Java Edition)');
  await expect(survival).not.toContainText('sha256');
  await expect(survival.getByRole('meter', { name: 'CPU' })).toHaveAttribute('aria-valuenow', '38');
  await expect(survival).toContainText('5.0 / 8.0 GB');
  await expect(survival).toContainText('Started 2 d ago');
  await expect(survival).toHaveAttribute('data-status', 'running');
  // Players come live over servers.overview.
  await expect(survival).toContainText('Players 2');
  await expect(card(page, 'creative')).toContainText('itzg/minecraft-server:java21');
  await expect(card(page, 'creative')).toHaveAttribute('data-status', 'restartLoop');
  await expect(card(page, 'arena')).toHaveAttribute('data-status', 'rconError');
  const lobby = card(page, 'lobby');
  await expect(lobby).toHaveAttribute('data-status', 'stopped');
  await expect(lobby.getByRole('meter')).toHaveCount(0);
  await expect(lobby).not.toContainText('Started');
  // The game version has no source in R1 (revision 2.4 candidate 11).
  await expect(page.getByRole('main')).not.toContainText('Version');
  // The groups frame the cards.
  await expect(page.getByRole('region', { name: 'minecraft' }).getByRole('article')).toHaveCount(3);
  await expect(page.getByRole('region', { name: 'events' }).getByRole('article')).toHaveCount(1);
});

test('moderator cards have no agent data (UI-006)', async ({ page, state }) => {
  for (const user of ['mod', 'sup']) {
    await page.context().clearCookies();
    await login(page, user, secret(state, user));
    const survival = card(page, 'survival');
    await expect(survival).toHaveAttribute('data-status', 'online');
    await expect(survival).toContainText('online');
    await expect(survival).toContainText('Profile: Minecraft (Java Edition)');
    await expect(survival).toContainText('Players 2');
    await expect(survival.getByRole('meter')).toHaveCount(0);
    await expect(survival).not.toContainText('Started');
    await expect(survival).not.toContainText('GB');
  }
});

test('the search narrows the cards', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await page.getByRole('searchbox', { name: 'Search servers by name' }).fill('ARE');
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(card(page, 'arena')).toBeVisible();
});
