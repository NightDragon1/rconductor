// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Masking settings of container logs (UI-022, D-088 c): the RCON admin sets the switch, fixed texts and limits;
// the texts are write-only, the page shows only their number.

// The masking settings are part of the group System (O8).
async function system(page: Page) {
  await page.goto('admin/settings');
  await page
    .getByRole('navigation', { name: 'Settings groups' })
    .getByRole('button', { name: 'System' })
    .click();
}

async function as(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

test.describe('log masking on', () => {
  test('the RCON admin stores a text; it is never shown again', async ({ page, state }) => {
    await as(page, state, 'admin');
    await system(page);
    const panel = page.getByRole('region', { name: 'Log masking' });
    await expect(panel.getByText('0 texts stored')).toBeVisible();
    await panel.getByLabel('Replace the texts, one per line').fill('e2e-s3cr3t-text');
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByRole('status')).toHaveText('Saved.');
    await expect(panel.getByText('1 text stored')).toBeVisible();
    await expect(panel.getByLabel('Replace the texts, one per line')).toHaveValue('');
    const r = await page.request.get('api/v1/settings/log-masking');
    expect(r.status()).toBe(200);
    expect(await r.text()).not.toContain('e2e-s3cr3t-text');

    // Misuse: a text under 4 characters is refused, nothing changes.
    await panel.getByLabel('Replace the texts, one per line').fill('abc');
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByRole('status')).not.toHaveText('Saved.');
    await expect(panel.getByRole('status')).not.toBeEmpty();
    await expect(panel.getByText('1 text stored')).toBeVisible();

    // Misuse: a limit that is not a whole number is refused, never saved.
    await panel.getByLabel('Replace the texts, one per line').fill('');
    await panel.getByLabel('Most texts').fill('abc');
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByRole('status')).toHaveText('The limits must be whole numbers.');
    await page.reload();
    await expect(panel.getByLabel('Most texts')).not.toHaveValue('abc');

    await panel.getByLabel('Remove all stored texts').check();
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByText('0 texts stored')).toBeVisible();
  });

  test('a changed setting meanwhile and a large input are refused', async ({ page, state }) => {
    await as(page, state, 'admin');
    await system(page);
    const panel = page.getByRole('region', { name: 'Log masking' });
    await expect(panel.getByText(/texts? stored/)).toBeVisible();

    // Misuse: the setting changed in another tab after the page read it (409).
    const before = await (await page.request.get('api/v1/settings/log-masking')).json();
    const put = await page.request.put('api/v1/settings/log-masking', {
      headers: { 'If-Match': String(before.version), Origin: new URL(page.url()).origin },
      data: { budget_ms: before.budget_ms + 1 },
    });
    expect(put.status()).toBe(200);
    await panel.getByLabel('Most texts').fill('7');
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByRole('status')).toHaveText('Changed by someone else – reload');
    const after = await (await page.request.get('api/v1/settings/log-masking')).json();
    expect(after.max_texts).toBe(before.max_texts);

    // Misuse: more texts than allowed and a text over the bound, nothing stored.
    await page.reload();
    await panel
      .getByLabel('Replace the texts, one per line')
      .fill(Array.from({ length: 250 }, (_, i) => `e2e-many-${i}`).join('\n'));
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByRole('status')).not.toHaveText('Saved.');
    await expect(panel.getByRole('status')).not.toBeEmpty();
    await panel.getByLabel('Replace the texts, one per line').fill('x'.repeat(5000));
    await panel.getByRole('button', { name: 'Save' }).click();
    await expect(panel.getByRole('status')).not.toHaveText('Saved.');
    await expect(panel.getByRole('status')).not.toBeEmpty();
    const last = await (await page.request.get('api/v1/settings/log-masking')).json();
    expect(last.text_count).toBe(after.text_count);
    expect(last.version).toBe(after.version);
  });

  test('on a phone the page part is read-only', async ({ page, state }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await login(page, 'admin', secret(state, 'admin'));
    await page.goto('admin/settings'); // a phone shows every group, without the group list
    const panel = page.getByRole('region', { name: 'Log masking' });
    await expect(panel.getByLabel('Most texts')).toBeDisabled();
    await expect(panel.getByLabel('Replace the texts, one per line')).toBeDisabled();
    await expect(panel.getByRole('button', { name: 'Save' })).toHaveCount(0);
  });

  test('a stored text masks the next live line, the tail and the system log (UI-022, D-088)', async ({
    browser,
    page,
    state,
  }) => {
    await as(page, state, 'admin');
    const origin = new URL(page.url()).origin;
    const store = async (texts: { value: string }[]) => {
      const m = await (await page.request.get('api/v1/settings/log-masking')).json();
      const r = await page.request.put('api/v1/settings/log-masking', {
        headers: { 'If-Match': String(m.version), Origin: origin },
        data: { enabled: true, texts },
      });
      expect(r.status()).toBe(200);
    };
    await store([{ value: 'Steve joined' }]);
    try {
      // Live lines and the tail of the container go through the same masker.
      await page.goto(`servers/${state.servers.survival}/logs`);
      const lines = page.getByRole('region', { name: 'Logs' }).locator('pre');
      await expect(lines).toContainText('••• the game');
      await expect(lines).not.toContainText('Steve joined');
      await page.getByRole('button', { name: 'Load earlier lines' }).click();
      await expect(lines).toContainText('Starting minecraft server version 26.3');
      await expect(lines).not.toContainText('Steve joined');

      // The system log too: a new user whose name holds the text.
      const created = await page.request.post('api/v1/invites', {
        data: { role: 'moderator' },
        headers: { Origin: origin },
      });
      expect(created.status()).toBe(201);
      const inv = await created.json();
      const anon = await browser.newContext({ baseURL: state.base_url });
      const name = `Steve joined ${Date.now() % 100000}`;
      const r = await anon.request.post('api/v1/invites/redeem', {
        data: { token: inv.token, pseudonym: name, password: 'Masked-Name-Pass-7!' },
        headers: { Origin: origin },
      });
      expect(r.status()).toBe(201);
      await anon.close();
      const texts = async () => {
        const out: { id: number; code?: string; text: string }[] = [];
        for (let after = 0; ;) {
          const res = await page.request.get(
            `api/v1/system-log?source=panel&limit=500${after ? `&after=${after}` : ''}`,
          );
          expect(res.status(), await res.text()).toBe(200);
          const items = (await res.json()).items as typeof out;
          out.push(...items);
          if (items.length < 500) return out;
          after = items[items.length - 1]?.id ?? 0;
        }
      };
      await expect
        .poll(async () => (await texts()).some((e) => e.code === 'auth.invite.used'), {
          timeout: 10_000,
        })
        .toBe(true);
      expect((await texts()).some((e) => e.text.includes('Steve joined'))).toBe(false);
    } finally {
      await store([]);
    }
  });

  test('a server admin has neither the page part nor the API', async ({ page, state }) => {
    await as(page, state, 'sadmin');
    await page.goto('admin/settings');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Log masking' })).toHaveCount(0);
    const r = await page.request.get('api/v1/settings/log-masking');
    expect(r.status()).toBe(403);
  });
});
