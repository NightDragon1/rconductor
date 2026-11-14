// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Page } from '@playwright/test';

type Playwright = { request: { newContext: (o: object) => Promise<APIRequestContext> } };
import { expect, login, secret, test, type StackState } from './fixtures';

// Command palette (CMD-016) behind the flag command_palette: a button in the
// console of the admins opens it, Ctrl+K only while keyboard shortcuts are
// on (UI-008); it lists only what the backend lets the role reach.

const button = (page: Page) => page.getByRole('button', { name: /^Command palette/ });
const search = (page: Page) =>
  page.getByRole('combobox', { name: 'Search commands, templates and favorites' });
const results = (page: Page) => page.getByRole('listbox', { name: 'Results' });

async function openConsole(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.server_id}/console`);
  await expect(page.getByRole('region', { name: 'Console' })).toBeVisible();
}

// shortcuts switches the global keyboard shortcuts as RCON admin (UI-008).
async function shortcuts(playwright: Playwright, state: StackState, on: boolean) {
  const admin = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
  });
  try {
    const r = await admin.post('api/v1/auth/login', {
      data: { pseudonym: 'admin', password: secret(state, 'admin') },
    });
    expect(r.status()).toBe(200);
    const etag = (await admin.get('api/v1/settings')).headers()['etag'] ?? '';
    const w = await admin.patch('api/v1/settings', {
      headers: { 'If-Match': etag },
      data: { 'ui.shortcuts_enabled': on },
    });
    expect(w.status()).toBe(200);
  } finally {
    await admin.dispose();
  }
}

test.describe('command palette on', () => {
  test.use({ flags: ['command_palette'] });

  test('the button opens it; a command goes into the input line', async ({ page, state }) => {
    await openConsole(page, state, 'sadmin');
    await button(page).click();
    await search(page).fill('kick');
    const kick = results(page).getByRole('option').filter({ hasText: 'kick' }).first();
    await expect(kick).toContainText('Command');
    await search(page).press('Enter');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Command' })).toHaveValue(/^kick/);
  });

  test('Ctrl+K only while shortcuts are on', async ({ page, playwright, state }) => {
    await openConsole(page, state, 'sadmin');
    await page.keyboard.press('Control+k');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await shortcuts(playwright, state, true);
    try {
      await page.reload();
      await expect(page.getByRole('region', { name: 'Console' })).toBeVisible();
      await page.keyboard.press('Control+k');
      await expect(search(page)).toBeVisible();
    } finally {
      await shortcuts(playwright, state, false);
    }
  });

  test('misuse: no console for moderators, scope, odd input', async ({ page, state }) => {
    // Moderators and supporters have no console, so no palette (F12).
    await login(page, 'mod', secret(state, 'mod'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(`servers/${state.server_id}/console`);
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
    await expect(button(page)).toHaveCount(0);
    // The server admin finds no server outside his scope.
    await openConsole(page, state, 'sadmin');
    await button(page).click();
    await search(page).fill('creative');
    await expect(page.getByRole('dialog')).toContainText('Nothing matches.');
    // Long input with control characters: no error, nothing matches.
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));
    await search(page).fill('\u0007\u202e' + 'k'.repeat(10 * 1024));
    await expect(page.getByRole('dialog')).toContainText('Nothing matches.');
    expect(errors).toEqual([]);
  });
});

test('command palette off: no button, Ctrl+K without effect', async ({ page, state }) => {
  await openConsole(page, state, 'sadmin');
  await expect(button(page)).toHaveCount(0);
  await page.keyboard.press('Control+k');
  await expect(page.getByRole('dialog')).toHaveCount(0);
});
