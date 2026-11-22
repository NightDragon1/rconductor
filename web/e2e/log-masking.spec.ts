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

  test('a server admin has neither the page part nor the API', async ({ page, state }) => {
    await as(page, state, 'sadmin');
    const r = await page.request.get('api/v1/settings/log-masking');
    expect(r.status()).toBe(403);
  });
});
