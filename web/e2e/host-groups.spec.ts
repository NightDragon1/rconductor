// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Overview by host (UI-021, D-089) behind the flag host_groups: the RCON
// admin names the host of agent host-1 on the agents page; every role sees
// the cards of its scope framed per host with that name as text, the
// servers without agent (outpost) apart; host name and address only for
// the RCON admin.

const display = 'Rack <b>1</b>';

async function as(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

const headers = (state: StackState) => ({ Origin: new URL(state.base_url).origin });

async function hostOne(page: Page) {
  const { items } = (await (await page.request.get('api/v1/agents')).json()) as {
    items: { id: string; name: string }[];
  };
  return items.find((a) => a.name === 'host-1')?.id ?? '';
}

test.describe('host groups on', () => {
  test.use({ flags: ['host_groups'] });

  test('the RCON admin names a host; the overview frames its cards', async ({ page, state }) => {
    await as(page, state, 'admin');
    const id = await hostOne(page);
    try {
      await page.goto('admin/agents');
      const row = page.getByRole('row').filter({ hasText: 'host-1' });
      await row.getByRole('button', { name: 'Display name' }).click();
      const dialog = page.getByRole('dialog');
      // Too long: the backend refuses, the dialog says so.
      await dialog.getByLabel('Display name').fill('x'.repeat(65));
      await dialog.getByRole('button', { name: 'Save' }).click();
      await expect(dialog.getByRole('alert')).toBeVisible();
      await dialog.getByLabel('Display name').fill(display);
      await dialog.getByRole('button', { name: 'Save' }).click();
      await expect(dialog).toBeHidden();
      await expect(row).toContainText(display);
      await expect(row.locator('b')).toHaveCount(0);

      await page.goto('');
      const host = page.getByRole('region', { name: display });
      await expect(host.getByRole('article', { name: 'survival' })).toBeVisible();
      await expect(host).toContainText(/\d+ servers? · running \d+ · stopped \d+ · error \d+/);
      await expect(host.locator('b')).toHaveCount(0);
      const none = page.getByRole('region', { name: 'Servers without agent' });
      await expect(none.getByRole('article', { name: 'outpost' })).toBeVisible();

      // Control characters are refused (SEC-006).
      const bad = await page.request.patch(`api/v1/agents/${id}`, {
        data: { display_name: 'Rack\u0007' },
        headers: headers(state),
      });
      expect(bad.status()).toBe(400);
    } finally {
      await page.request.patch(`api/v1/agents/${id}`, {
        data: { display_name: null },
        headers: headers(state),
      });
    }
  });

  test('a moderator sees the display name, never the host, and cannot set it', async ({
    page,
    state,
  }) => {
    await as(page, state, 'admin');
    const id = await hostOne(page);
    const set = (name: string | null) =>
      page.request.patch(`api/v1/agents/${id}`, {
        data: { display_name: name },
        headers: headers(state),
      });
    expect((await set(display)).status()).toBe(200);
    try {
      await page.context().clearCookies();
      await as(page, state, 'mod');
      const host = page.getByRole('region', { name: display });
      await expect(host.getByRole('article', { name: 'survival' })).toBeVisible();
      const body = await (await page.request.get('api/v1/servers')).text();
      expect(body).toContain('"host_group"');
      expect(body).not.toContain('host-1');
      expect(body).not.toContain(id);
      expect((await set('Mine')).status()).toBe(403);
    } finally {
      await page.context().clearCookies();
      await as(page, state, 'admin');
      await set(null);
    }
  });
});

test('host groups off: overview as before, no display name', async ({ page, state }) => {
  await as(page, state, 'admin');
  const id = await hostOne(page);
  await expect(page.getByRole('article', { name: 'survival' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Servers without agent' })).toHaveCount(0);
  const r = await page.request.patch(`api/v1/agents/${id}`, {
    data: { display_name: 'Rack' },
    headers: headers(state),
  });
  expect(r.status()).toBe(404);
  await page.goto('admin/agents');
  await expect(page.getByRole('columnheader', { name: 'Display name' })).toHaveCount(0);
});
