// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Server number (UI-018, D-087) behind the flag server_number: first
// column ID in Servers & Groups, badge before the name on cards and the
// server page; a number is never given twice.

async function as(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

test.describe('server number on', () => {
  test.use({ flags: ['server_number'] });

  test('ID column, badge on card and server page; a deleted number comes not back', async ({
    page,
    state,
  }) => {
    await as(page, state, 'admin');
    const survival = await (
      await page.request.get(`api/v1/servers/${state.servers.survival}`)
    ).json();
    expect(survival.number).toBeGreaterThan(0);
    const badge = `Server number ${survival.number}`;

    await expect(page.getByRole('article', { name: 'survival' }).getByLabel(badge)).toHaveText(
      `#${survival.number}`,
    );
    await page.goto(`servers/${state.servers.survival}/overview`);
    await expect(page.getByRole('heading', { level: 1 }).getByLabel(badge)).toBeVisible();
    await page.goto('admin/servers');
    await expect(page.getByRole('columnheader').first()).toHaveText('ID');
    await expect(
      page.getByRole('row').filter({ hasText: 'survival' }).getByRole('cell').first(),
    ).toHaveText(String(survival.number));

    // Delete and create again: a new, higher number (D-087).
    const headers = { Origin: new URL(state.base_url).origin };
    const body = (name: string) => ({
      data: {
        name,
        profile_ids: survival.profile_ids,
        host: 'num.lan',
        port: 25575,
        password: 'e2e-rcon',
      },
      headers,
    });
    const first = await (await page.request.post('api/v1/servers', body('num-a'))).json();
    expect((await page.request.delete(`api/v1/servers/${first.id}`, { headers })).status()).toBe(
      204,
    );
    const again = await page.request.post('api/v1/servers', body('num-a'));
    expect(again.status()).toBe(201);
    const second = await again.json();
    try {
      expect(second.number).toBeGreaterThan(first.number);
    } finally {
      await page.request.delete(`api/v1/servers/${second.id}`, { headers });
    }
  });

  test('a moderator sees the badge of his server too', async ({ page, state }) => {
    await as(page, state, 'mod');
    const s = await (await page.request.get(`api/v1/servers/${state.servers.survival}`)).json();
    await expect(
      page.getByRole('article', { name: 'survival' }).getByLabel(`Server number ${s.number}`),
    ).toBeVisible();
  });
});

test('server number off: no column ID and no badge', async ({ page, state }) => {
  await as(page, state, 'admin');
  await expect(page.getByRole('article', { name: 'survival' }).getByText(/^#\d+$/)).toHaveCount(0);
  await page.goto('admin/servers');
  await expect(page.getByRole('columnheader', { name: 'ID', exact: true })).toHaveCount(0);
});
