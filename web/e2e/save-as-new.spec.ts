// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Save as New (UI-017) behind the flag server_save_as_new: after create the
// server dialog stays open; Save changes the server just created, Save as
// New creates another one with the same data and the password typed again;
// once all is saved it closes without a question.

type Playwright = { request: { newContext: (o: object) => Promise<APIRequestContext> } };

async function as(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('admin/servers');
  await expect(page.getByRole('heading', { name: 'Servers and groups', level: 1 })).toBeVisible();
}

async function createOne(page: Page, name: string) {
  await page.getByRole('button', { name: 'New server' }).click();
  const panel = page.getByRole('complementary', { name: 'Details of New server' });
  await panel.getByLabel('Name').fill(name);
  await panel.getByLabel('Add a game profile').selectOption({ label: 'Minecraft (Java Edition)' });
  await panel.getByLabel('Host').fill('mc.example');
  await panel.getByLabel('RCON port').fill('25575');
  await panel.getByLabel('RCON password').fill('a secret rcon password');
  await panel.getByRole('button', { name: 'Create server' }).click();
}

// cleanup deletes the servers of this spec through the API as RCON admin.
async function cleanup(playwright: Playwright, state: StackState, prefix: string) {
  const api = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
  });
  try {
    await api.post('api/v1/auth/login', {
      data: { pseudonym: 'admin', password: secret(state, 'admin') },
    });
    const { items } = (await (await api.get('api/v1/servers')).json()) as {
      items: { id: string; name: string }[];
    };
    for (const s of items.filter((x) => x.name.startsWith(prefix))) {
      const one = await api.get(`api/v1/servers/${s.id}`);
      const d = await api.delete(`api/v1/servers/${s.id}`, {
        headers: { 'If-Match': one.headers()['etag'] ?? '' },
      });
      expect(d.ok(), `delete ${s.name}: ${d.status()}`).toBe(true);
    }
  } finally {
    await api.dispose();
  }
}

test.describe('Save as New on', () => {
  test.use({ flags: ['server_save_as_new'] });

  test('dialog stays open; Save changes it, Save as New adds one', async ({
    page,
    playwright,
    state,
  }) => {
    try {
      await as(page, state, 'admin');
      await createOne(page, 'e2e-san-a');
      const panel = page.getByRole('complementary', { name: 'Details of e2e-san-a' });
      await expect(panel).toBeVisible();
      await expect(
        page.getByRole('status').filter({ hasText: 'Server e2e-san-a created.' }),
      ).toBeVisible();
      // Save changes the server just created.
      await panel.getByLabel('Host').fill('mc2.example');
      await panel.getByRole('button', { name: 'Save', exact: true }).click();
      await expect(page.getByRole('row').filter({ hasText: 'e2e-san-a' })).toContainText(
        'mc2.example:25575',
      );
      // Save as New wants the password again; it is not taken from the answer.
      await panel.getByLabel('Name').fill('e2e-san-b');
      await expect(panel.getByLabel('New RCON password')).toHaveValue('');
      await panel.getByRole('button', { name: 'Save as New' }).click();
      await expect(page.getByRole('alert')).toHaveText(
        'Enter the RCON password again for the new server.',
      );
      await panel.getByLabel('New RCON password').fill('another rcon password');
      await panel.getByRole('button', { name: 'Save as New' }).click();
      await expect(
        page.getByRole('status').filter({ hasText: 'Server e2e-san-b created as a new server.' }),
      ).toBeVisible();
      await expect(page.getByRole('row').filter({ hasText: 'e2e-san-a' })).toHaveCount(1);
      await expect(page.getByRole('row').filter({ hasText: 'e2e-san-b' })).toContainText(
        'mc2.example:25575',
      );
      // All saved: closing asks nothing.
      await page.getByRole('button', { name: 'Close the details' }).click();
      await expect(page.getByRole('complementary', { name: /^Details of / })).toHaveCount(0);
      await expect(page.getByText('Leave without saving?')).toHaveCount(0);
    } finally {
      await cleanup(playwright, state, 'e2e-san-');
    }
  });

  test('misuse: a double click creates exactly one; no Save as New for the server admin', async ({
    page,
    playwright,
    state,
  }) => {
    try {
      await as(page, state, 'admin');
      await createOne(page, 'e2e-san-c');
      const panel = page.getByRole('complementary', { name: 'Details of e2e-san-c' });
      await panel.getByLabel('Name').fill('e2e-san-d');
      await panel.getByLabel('New RCON password').fill('another rcon password');
      const creates: string[] = [];
      page.on('request', (r) => {
        if (r.method() === 'POST' && r.url().endsWith('/api/v1/servers')) creates.push(r.url());
      });
      await panel.getByRole('button', { name: 'Save as New' }).dblclick();
      await expect(
        page.getByRole('status').filter({ hasText: 'Server e2e-san-d created as a new server.' }),
      ).toBeVisible();
      await expect(page.getByRole('row').filter({ hasText: 'e2e-san-d' })).toHaveCount(1);
      expect(creates).toHaveLength(1);

      // The server admin may not create servers: no New server, no Save as New.
      await as(page, state, 'sadmin');
      await expect(page.getByRole('button', { name: 'New server' })).toHaveCount(0);
      await page.getByRole('button', { name: 'survival', exact: true }).click();
      const own = page.getByRole('complementary', { name: 'Details of survival' });
      await expect(own.getByRole('button', { name: 'Save as New' })).toHaveCount(0);
    } finally {
      await cleanup(playwright, state, 'e2e-san-');
    }
  });
});

test('Save as New off: the dialog closes after create', async ({ page, playwright, state }) => {
  try {
    await as(page, state, 'admin');
    await createOne(page, 'e2e-san-off');
    await expect(page.getByRole('row').filter({ hasText: 'e2e-san-off' })).toBeVisible();
    await expect(page.getByRole('complementary', { name: /^Details of / })).toHaveCount(0);
  } finally {
    await cleanup(playwright, state, 'e2e-san-');
  }
});
