// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

const consoleOf = (page: Page) => page.getByRole('region', { name: 'Console' });
const input = (page: Page) => page.getByRole('textbox', { name: 'Command' });
const block = (page: Page, command: string) =>
  consoleOf(page).getByRole('article', { name: command, exact: true });

async function openConsole(page: Page, state: StackState, server: string) {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers[server]}/console`);
  await expect(consoleOf(page)).toBeVisible();
}

async function send(page: Page, command: string) {
  await input(page).fill(command);
  await input(page).press('Enter');
  await expect(block(page, command).last()).toBeVisible();
}

test('blocks with time, user and role; collapse and copy (CMD-012)', async ({
  page,
  state,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await openConsole(page, state, 'survival');
  // The history comes first (G1).
  await expect(block(page, 'say hi from mod')).toBeVisible();
  await send(page, 'list');
  const b = block(page, 'list').last();
  await expect(b).toContainText('admin');
  await expect(b).toContainText('RCON admin');
  await expect(b).toContainText('There are 2 of a max of 20 players online');
  await expect(b.locator('time')).toHaveText(/\d{1,2}:\d{2}/);
  await b.getByRole('button', { name: 'Collapse' }).click();
  await expect(b).not.toContainText('players online');
  await b.getByRole('button', { name: 'Expand' }).click();
  await b.getByRole('button', { name: 'Copy the block' }).click();
  await expect(b).toContainText('Copied');
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied).toContain('list\nThere are 2 of a max of 20 players online');
});

test('timeline markers jump to their block; errors are red (CMD-013)', async ({ page, state }) => {
  await openConsole(page, state, 'arena');
  // arena rejects the RCON password: the command fails.
  await input(page).fill('list');
  await input(page).press('Enter');
  const failed = block(page, 'list').last();
  await expect(failed).toHaveAttribute('data-tone', 'error');
  const timeline = page.getByRole('navigation', { name: 'Timeline' });
  const markers = timeline.getByRole('button');
  // One marker per block: commands and log deliveries. Tick lines keep
  // coming, so both are counted at the same moment.
  await expect(consoleOf(page).getByRole('region', { name: 'Log lines' }).first()).toBeVisible();
  const counts = await page.evaluate(() => {
    const c = document.querySelector('[aria-label="Console"]');
    return {
      blocks: c?.querySelectorAll('article, section[aria-label="Log lines"]').length ?? -1,
      markers: c?.querySelectorAll('nav[aria-label="Timeline"] button').length ?? -2,
    };
  });
  expect(counts.markers).toBe(counts.blocks);
  await expect(markers.last()).toHaveAttribute('data-tone', 'error');
  await expect(markers.last()).toHaveClass(/bg-danger/);

  await page.goto(`servers/${state.servers.survival}/console`);
  for (let i = 0; i < 12; i++) await send(page, `say marker ${i}`);
  const first = page.getByRole('navigation', { name: 'Timeline' }).getByRole('button').first();
  const name = (await first.getAttribute('title')) ?? '';
  await first.click();
  await expect(block(page, name).first()).toBeInViewport();
});

test('tracks and filter; others live (CMD-014)', async ({ page, state, browser }) => {
  await openConsole(page, state, 'survival');
  // Logs of the agent arrive while the track is on (G2).
  const logs = consoleOf(page).getByRole('region', { name: 'Log lines' });
  await expect(logs.first()).toContainText('Steve joined the game');
  await expect(logs.first()).toContainText('Done (1.2s)!');
  await expect(logs.first()).not.toContainText('§');

  // A moderator's command shows up live.
  const mod = await browser.newContext();
  const modPage = await mod.newPage();
  await login(modPage, 'mod', secret(state, 'mod'));
  await expect(modPage.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const status = await modPage.evaluate(
    (id) =>
      fetch(`api/v1/servers/${id}/commands`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'say live from mod' }),
      }).then((r) => r.status),
    state.servers.survival,
  );
  expect(status).toBe(200);
  await mod.close();
  const live = block(page, 'say live from mod');
  await expect(live).toBeVisible();
  await expect(live).toHaveAttribute('data-tone', 'others');

  const track = (name: string) => consoleOf(page).getByRole('button', { name, exact: true });
  await track("Others' commands").click();
  await expect(live).toHaveCount(0);
  await track("Others' commands").click();
  await track('Logs').click();
  await expect(logs).toHaveCount(0);
  await track('Logs').click();
  await track('Own commands').click();
  await expect(consoleOf(page).getByRole('article').filter({ hasText: 'RCON admin' })).toHaveCount(
    0,
  );
  await track('Own commands').click();

  await page.getByRole('searchbox', { name: 'Filter the console' }).fill('live from');
  await expect(consoleOf(page).getByRole('article')).toHaveCount(1);
});

test('an answer stays with its server across a switch (CMD-001)', async ({ page, state }) => {
  await openConsole(page, state, 'creative');
  const star = page.getByRole('button', { name: 'Add to favorites' });
  if (await star.count()) await star.click();
  await page.goto(`servers/${state.servers.survival}/console`);
  const star2 = page.getByRole('button', { name: 'Add to favorites' });
  if (await star2.count()) await star2.click();
  const bar = page.getByRole('navigation', { name: 'Favorites' });

  // The answer comes after a second, the user is on another server by then.
  await input(page).fill('say slowly to survival');
  await input(page).press('Enter');
  await bar.getByRole('link', { name: 'creative' }).click();
  await expect(page.getByRole('heading', { name: 'creative', level: 1 })).toBeVisible();

  // A draft stays with its server, and the input here is free at once.
  await expect(input(page)).toHaveValue('');
  await input(page).fill('a draft for creative');
  await page.waitForTimeout(1500);
  await expect(block(page, 'say slowly to survival')).toHaveCount(0);

  await bar.getByRole('link', { name: 'survival' }).click();
  await expect(block(page, 'say slowly to survival').last()).toContainText('Done:');
  await expect(input(page)).toHaveValue('');
  await bar.getByRole('link', { name: 'creative' }).click();
  await expect(input(page)).toHaveValue('a draft for creative');
  await input(page).fill('');
});

test('completion with Tab and own commands on arrow up (CMD-015)', async ({ page, state }) => {
  await openConsole(page, state, 'survival');
  await send(page, 'say first');
  await input(page).fill('whi');
  const list = page.getByRole('list', { name: 'Suggestions' });
  await expect(list.getByRole('listitem').first()).toContainText('whitelist add');
  await input(page).press('Tab');
  await expect(input(page)).toHaveValue('whitelist add ');
  await expect(input(page)).toBeFocused();
  await input(page).fill('');
  await input(page).press('ArrowUp');
  await expect(input(page)).toHaveValue('say first');
  await input(page).press('ArrowDown');
  await expect(input(page)).toHaveValue('');
});

test('favorites switch servers without a reload (CMD-001)', async ({ page, state }) => {
  await openConsole(page, state, 'creative');
  const star = page.getByRole('button', { name: 'Add to favorites' });
  if (await star.count()) await star.click();
  await expect(page.getByRole('button', { name: 'Remove from favorites' })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/console`);
  const star2 = page.getByRole('button', { name: 'Add to favorites' });
  if (await star2.count()) await star2.click();
  await send(page, 'say on survival');
  await page.evaluate(() => {
    (window as unknown as { marker: number }).marker = 42;
  });
  const bar = page.getByRole('navigation', { name: 'Favorites' });
  await bar.getByRole('link', { name: 'creative' }).click();
  await expect(page).toHaveURL(new RegExp(`servers/${state.servers.creative}/console$`));
  await expect(page.getByRole('heading', { name: 'creative', level: 1 })).toBeVisible();
  await send(page, 'say on creative');
  await expect(block(page, 'say on survival')).toHaveCount(0);
  await bar.getByRole('link', { name: 'survival' }).click();
  await expect(block(page, 'say on survival').last()).toBeVisible();
  await bar.getByRole('link', { name: 'creative' }).click();
  await expect(block(page, 'say on creative').last()).toBeVisible();
  // No reload happened: the page's state is still there.
  expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(42);
});
