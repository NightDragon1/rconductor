// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// System log (UI-025, D-091) and the diagnostic export (NFR-018) behind
// the flag diagnostics_export: only the RCON admin sees the page.

const title = 'System log';
const notice = 'May contain chat messages of players.';

async function api(
  playwright: { request: { newContext: (o: object) => Promise<APIRequestContext> } },
  state: StackState,
  user: string,
) {
  const c = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
  });
  const r = await c.post('api/v1/auth/login', {
    data: { pseudonym: user, password: secret(state, user) },
  });
  expect(r.status()).toBe(200);
  return c;
}

const entry = (page: Page) => page.getByRole('navigation').getByRole('link', { name: title });

test.describe('system log on', () => {
  test.use({ flags: ['diagnostics_export'] });

  test('admin opens the page, filters, sees the notice and a refusal', async ({ page, state }) => {
    await login(page, 'admin', secret(state, 'admin'));
    await entry(page).click();
    await expect(page.getByRole('heading', { name: title, level: 1 })).toBeVisible();
    await expect(page.getByRole('note')).toHaveText(notice);
    const source = page.getByLabel('Source');
    await expect(source.locator('option[value="panel"]')).toHaveText('Panel');
    const level = page.getByLabel('Level');
    await level.selectOption('error');
    await expect(level).toHaveValue('error');
    await page.getByLabel('Period').selectOption('week');
    // The backend does not answer yet: the page names the refusal and stays.
    const entries = page.getByRole('region', { name: 'Entries' });
    await expect(entries.getByRole('alert')).toBeVisible();
    await page.getByRole('button', { name: 'Download diagnostic package' }).click();
    // The export names its refusal next to the one of the entries.
    await expect(page.getByRole('alert')).toHaveCount(2);
    await expect(page.getByRole('heading', { name: title, level: 1 })).toBeVisible();
  });

  test('misuse: a moderator has no entry and no page, oversized input is refused', async ({
    page,
    playwright,
    state,
  }) => {
    await login(page, 'mod', secret(state, 'mod'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await expect(entry(page)).toHaveCount(0);
    await page.goto('admin/system-log');
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
    await expect(page.getByText(notice)).toHaveCount(0);

    const mod = await api(playwright, state, 'mod');
    try {
      for (const path of ['api/v1/system-log', 'api/v1/diagnostics']) {
        const r = await mod.get(path);
        expect([403, 404], `${path} ${r.status()}`).toContain(r.status());
      }
    } finally {
      await mod.dispose();
    }
    const admin = await api(playwright, state, 'admin');
    try {
      const long = await admin.get(`api/v1/system-log?source=${'x'.repeat(10 * 1024)}`);
      expect([400, 414, 431], `long source ${long.status()}`).toContain(long.status());
      expect((await admin.get('api/v1/system-log?limit=100000')).status()).toBe(400);
    } finally {
      await admin.dispose();
    }
  });
});

test('system log off: no menu entry, no page, 404', async ({ page, playwright, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await expect(entry(page)).toHaveCount(0);
  await page.goto('admin/system-log');
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  const admin = await api(playwright, state, 'admin');
  try {
    expect((await admin.get('api/v1/system-log')).status()).toBe(404);
    expect((await admin.get('api/v1/diagnostics')).status()).toBe(404);
  } finally {
    await admin.dispose();
  }
});
