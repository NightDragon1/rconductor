// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Two-part log filter (UI-019) behind the flag log_filter_split: the bracket
// part and the text after it, joined with and, or, xor.

const bracket = (page: Page) => page.getByRole('searchbox', { name: 'Filter the bracket part' });
const text = (page: Page) =>
  page.getByRole('searchbox', { name: 'Filter the text after the brackets' });
const lines = (page: Page) => page.getByRole('region', { name: 'Logs' }).locator('pre');

async function openLogs(page: Page, state: StackState) {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/logs`);
  await expect(lines(page)).toContainText('Steve joined the game');
  await expect(lines(page)).toContainText(/tick \d+/, { timeout: 10_000 });
}

test.describe('two-part log filter on', () => {
  test.use({ flags: ['log_filter_split'] });

  test('bracket part and text with and, or, xor', async ({ page, state }) => {
    await openLogs(page, state);
    await expect(page.getByRole('searchbox', { name: 'Filter the logs' })).toHaveCount(0);
    await bracket(page).fill('server thread');
    await text(page).fill('joined');
    await expect(lines(page)).toContainText('Steve joined');
    await expect(lines(page)).not.toContainText('tick');
    const op = page.getByRole('combobox', { name: 'Combine the two filters' });
    // Every line here has the bracket part: xor keeps the lines without the text.
    await op.selectOption('xor');
    await expect(lines(page)).toContainText('tick');
    await expect(lines(page)).not.toContainText('joined');
    await op.selectOption('or');
    await expect(lines(page)).toContainText('tick');
    await expect(lines(page)).toContainText('joined');
  });

  test('misuse: control characters and long input give a hint, no crash', async ({
    page,
    state,
  }) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));
    await openLogs(page, state);
    await bracket(page).fill('a\u0007b');
    await expect(page.getByText(/contains control characters/)).toBeVisible();
    await expect(lines(page)).toContainText('tick');
    await bracket(page).fill('x'.repeat(10_000));
    await expect(page.getByText(/more than \d+ characters/)).toBeVisible();
    await expect(lines(page)).toContainText('Steve joined');
    await bracket(page).fill('');
    // No expression syntax: an odd text is a plain substring, it matches none.
    await text(page).fill('[unclosed (');
    await expect(page.getByRole('region', { name: 'Logs' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Logs' })).not.toContainText(/tick \d+/);
    expect(errors).toEqual([]);
  });
});

test('two-part log filter off: one filter', async ({ page, state }) => {
  await openLogs(page, state);
  await expect(page.getByRole('searchbox', { name: 'Filter the logs' })).toBeVisible();
  await expect(bracket(page)).toHaveCount(0);
});
