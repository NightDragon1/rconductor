// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Profile browser (UI-020) behind the flag profile_browser: every role with
// access to a server reads the profiles and the effective profile of its
// servers; the game ports only the RCON admin and the server admin in scope
// (D-056); read only.

const effective = 'Effective profile of a server';

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

async function openBrowser(page: Page) {
  await page.getByRole('navigation').getByRole('link', { name: 'Game profiles' }).click();
  await expect(page.getByRole('heading', { name: 'Game profiles', level: 1 })).toBeVisible();
}

test.describe('profile browser on', () => {
  test.use({ flags: ['profile_browser'] });

  test('admin searches the catalog and reads the effective profile with ports', async ({
    page,
    state,
  }) => {
    await login(page, 'admin', secret(state, 'admin'));
    await openBrowser(page);
    const all = page.getByRole('list', { name: 'All game profiles' });
    await expect(all).toContainText('Minecraft (Java Edition)');
    await page.getByRole('searchbox', { name: /Search game profiles/ }).fill('factorio');
    await expect(all).toContainText('Factorio');
    await expect(all).not.toContainText('Minecraft (Java Edition)');

    const view = page.getByRole('region', { name: effective });
    await view.getByLabel('Server').selectOption({ label: state.server_name });
    const shown = view.getByTestId('effective');
    await expect(shown.getByRole('list', { name: 'Profiles, top first' })).toBeVisible();
    await expect(shown.getByText('Game ports')).toBeVisible();
    await expect(shown.getByRole('list', { name: 'Effective commands' })).toContainText('kick');
  });

  test('moderator sees the effective profile without ports', async ({ page, state }) => {
    await login(page, 'mod', secret(state, 'mod'));
    await openBrowser(page);
    const view = page.getByRole('region', { name: effective });
    await view.getByLabel('Server').selectOption({ label: state.server_name });
    const shown = view.getByTestId('effective');
    await expect(shown.getByRole('list', { name: 'Effective commands' })).toBeVisible();
    await expect(shown.getByText('Game ports')).toHaveCount(0);
  });

  test('misuse: no access, ports by role, read only, long search', async ({
    page,
    playwright,
    state,
  }) => {
    const path = `api/v1/servers/${state.server_id}/profile`;
    // A server outside the moderator's scope does not exist for him (404).
    const mod = await api(playwright, state, 'mod');
    try {
      expect((await mod.get(`api/v1/servers/${state.servers.creative}/profile`)).status()).toBe(
        404,
      );
      const own = (await (await mod.get(path)).json()) as Record<string, unknown>;
      expect(own).not.toHaveProperty('game_ports');
      // Read only: no write method on the view.
      for (const m of ['POST', 'PUT', 'PATCH', 'DELETE'] as const) {
        const r = await mod.fetch(path, { method: m, data: {} });
        expect([404, 405], `${m} ${r.status()}`).toContain(r.status());
      }
    } finally {
      await mod.dispose();
    }
    // Ports for the server admin in scope and the supporter has none (D-056).
    for (const [user, ports] of [
      ['sadmin', true],
      ['sup', false],
    ] as const) {
      const c = await api(playwright, state, user);
      try {
        const body = (await (await c.get(path)).json()) as Record<string, unknown>;
        expect('game_ports' in body, `${user} game_ports`).toBe(ports);
      } finally {
        await c.dispose();
      }
    }
    // 10 KiB of input with control characters: no error, the page stays.
    await login(page, 'admin', secret(state, 'admin'));
    await openBrowser(page);
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));
    await page
      .getByRole('searchbox', { name: /Search game profiles/ })
      .fill('\u0007‮' + 'x'.repeat(10 * 1024));
    await expect(page.getByRole('region', { name: 'All game profiles' })).toContainText(
      'Nothing matches.',
    );
    expect(errors).toEqual([]);
  });
});

test('profile browser off: no menu entry and 404', async ({ page, playwright, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await expect(
    page.getByRole('navigation').getByRole('link', { name: 'Game profiles' }),
  ).toHaveCount(0);
  const mod = await api(playwright, state, 'mod');
  try {
    expect((await mod.get(`api/v1/servers/${state.server_id}/profile`)).status()).toBe(404);
  } finally {
    await mod.dispose();
  }
});
