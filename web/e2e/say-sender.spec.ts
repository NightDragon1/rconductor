// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Say with sender (CMD-020, D-082): the prefix
// of messages to the players is set per server group in the server
// administration. survival (profile minecraft-java, in the group
// minecraft) answers every command with "Done: <command>"
// (internal/e2e/stack), so a say shows as the tellraw the panel sent. Every
// test puts the prefixes back through the API, also when it fails.

type Playwright = { request: { newContext: (o: object) => Promise<APIRequestContext> } };
type Group = { id: string; name: string; server_ids: string[] };

const field = (page: Page) => groupPanel(page).getByLabel('Message prefix');
const groupPanel = (page: Page) =>
  page.getByRole('complementary', { name: 'Details of minecraft' });

async function as(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

async function openGroup(page: Page, name = 'minecraft') {
  await page.goto('admin/servers');
  await expect(
    page.getByRole('heading', { name: 'Servers and server groups', level: 1 }),
  ).toBeVisible();
  await page
    .getByRole('region', { name: 'Server groups' })
    .getByRole('button', { name: new RegExp(`^${name}$`, 'i') })
    .click();
}

// adminApi is an API session of the RCON admin for setup and cleanup.
async function adminApi(playwright: Playwright, state: StackState) {
  const api = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
  });
  const r = await api.post('api/v1/auth/login', {
    data: { pseudonym: 'admin', password: secret(state, 'admin') },
  });
  expect(r.status(), 'admin login').toBe(200);
  return api;
}

async function groupsOf(api: APIRequestContext): Promise<Group[]> {
  return ((await (await api.get('api/v1/server-groups')).json()) as { items: Group[] }).items;
}

async function readPrefix(api: APIRequestContext, id: string) {
  const r = await api.get(`api/v1/server-groups/${id}/say-prefix`);
  expect(r.status(), 'read prefix').toBe(200);
  return (await r.json()) as { say_prefix: string; version: string };
}

async function setPrefix(api: APIRequestContext, id: string, value: string) {
  const { version } = await readPrefix(api, id);
  const r = await api.put(`api/v1/server-groups/${id}/say-prefix`, {
    data: { say_prefix: value },
    headers: { 'If-Match': version },
  });
  expect(r.status(), `set prefix ${value}`).toBe(200);
}

// cleanup clears the prefix of minecraft and deletes the groups of this
// spec.
async function cleanup(playwright: Playwright, state: StackState) {
  const api = await adminApi(playwright, state);
  try {
    for (const g of await groupsOf(api)) {
      if (g.name === 'minecraft') await setPrefix(api, g.id, '');
      if (g.name.startsWith('aaa-e2e-say')) {
        const one = await api.get(`api/v1/server-groups/${g.id}`);
        const d = await api.delete(`api/v1/server-groups/${g.id}`, {
          headers: { 'If-Match': one.headers()['etag'] ?? '' },
        });
        expect(d.ok(), `delete ${g.name}: ${d.status()}`).toBe(true);
      }
    }
  } finally {
    await api.dispose();
  }
}

test.describe('say with sender on', () => {
  test('the RCON admin sets a prefix; a say reaches the server as tellraw', async ({
    page,
    playwright,
    state,
  }) => {
    try {
      await as(page, state, 'admin');
      await openGroup(page);
      await expect(field(page)).toHaveValue('');
      await field(page).fill('[R]');
      await groupPanel(page).getByRole('button', { name: 'Save prefix' }).click();
      await expect(groupPanel(page).getByRole('status')).toHaveText('Prefix saved.');

      await page.reload();
      await openGroup(page);
      await expect(field(page)).toHaveValue('[R]');

      await page.goto(`servers/${state.server_id}/console`);
      const input = page.getByRole('textbox', { name: 'Command' });
      await input.fill('say hello e2e-say');
      await input.press('Enter');
      const block = page
        .getByRole('region', { name: 'Console' })
        .getByRole('article')
        .filter({ hasText: 'hello e2e-say' })
        .last();
      await expect(block).toContainText(
        'Done: tellraw @a {"text":"","extra":[{"text":"[R] "},{"text":"admin: "},{"text":"hello e2e-say"}]}',
      );

      // Clear removes it again.
      await openGroup(page);
      await groupPanel(page).getByRole('button', { name: 'Clear prefix' }).click();
      await expect(groupPanel(page).getByRole('status')).toHaveText('Prefix cleared.');
      await expect(field(page)).toHaveValue('');
    } finally {
      await cleanup(playwright, state);
    }
  });

  test('a server in several groups: the hint names the first group by name', async ({
    page,
    playwright,
    state,
  }) => {
    const api = await adminApi(playwright, state);
    try {
      const r = await api.post('api/v1/server-groups', {
        data: { name: 'aaa-e2e-say', server_ids: [state.server_id] },
      });
      expect(r.status(), 'create group').toBe(201);
      const { id } = (await r.json()) as Group;
      await setPrefix(api, id, '[F]');
      const mc = (await groupsOf(api)).find((g) => g.name === 'minecraft');
      await setPrefix(api, mc?.id ?? '', '[M]');

      await as(page, state, 'admin');
      await openGroup(page);
      await expect(field(page)).toHaveValue('[M]');
      const hint = groupPanel(page).getByTestId('say-prefix-several');
      await expect(hint).toContainText(
        'Servers in several server groups use the prefix of the first server group by name.',
      );
      await expect(hint).toContainText('survival uses the prefix of aaa-e2e-say.');
    } finally {
      await api.dispose();
      await cleanup(playwright, state);
    }
  });

  test('misuse: § or 17 characters are refused and nothing is saved; no access for a moderator', async ({
    page,
    playwright,
    state,
  }) => {
    try {
      await as(page, state, 'admin');
      await openGroup(page);
      const save = groupPanel(page).getByRole('button', { name: 'Save prefix' });
      for (const bad of ['§a[R]', 'x'.repeat(17)]) {
        await field(page).fill(bad);
        await expect(groupPanel(page)).toContainText('At most 16 printable characters and no §');
        await save.click();
        await expect(groupPanel(page).getByRole('alert')).toHaveText(
          'Not saved: at most 16 printable characters, no §.',
        );
      }
      const api = await adminApi(playwright, state);
      try {
        const mc = (await groupsOf(api)).find((g) => g.name === 'minecraft');
        expect((await readPrefix(api, mc?.id ?? '')).say_prefix).toBe('');

        // A moderator has no server administration and the API refuses him.
        const mod = await playwright.request.newContext({
          baseURL: state.base_url,
          extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
        });
        try {
          await mod.post('api/v1/auth/login', {
            data: { pseudonym: 'mod', password: secret(state, 'mod') },
          });
          expect((await mod.get(`api/v1/server-groups/${mc?.id}/say-prefix`)).status()).toBe(403);
          const put = await mod.put(`api/v1/server-groups/${mc?.id}/say-prefix`, {
            data: { say_prefix: '[X]' },
            headers: { 'If-Match': '1' },
          });
          expect(put.status()).toBe(403);
        } finally {
          await mod.dispose();
        }
        expect((await readPrefix(api, mc?.id ?? '')).say_prefix).toBe('');
      } finally {
        await api.dispose();
      }
      await as(page, state, 'mod');
      await page.goto('admin/servers');
      await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
      await expect(page.getByLabel('Message prefix')).toHaveCount(0);
    } finally {
      await cleanup(playwright, state);
    }
  });
});
