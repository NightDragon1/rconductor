// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext } from '@playwright/test';
import { test, expect, secret, type StackState } from './fixtures';

// The flag switch of the e2e setup (NFR-019): a test that names a flag gets
// it on through the settings API, and it is off again after the test.

async function adminAPI(
  playwright: { request: { newContext: (o: object) => Promise<APIRequestContext> } },
  state: StackState,
) {
  const api = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
  });
  const r = await api.post('api/v1/auth/login', {
    data: { pseudonym: 'admin', password: secret(state, 'admin') },
  });
  expect(r.status()).toBe(200);
  return api;
}

test.describe('with the flag', () => {
  test.use({ flags: ['profile_browser'] });

  test('the guarded endpoint answers', async ({ playwright, state }) => {
    const api = await adminAPI(playwright, state);
    try {
      expect((await api.get(`api/v1/servers/${state.server_id}/profile`)).status()).toBe(200);
      const ui = (await (await api.get('api/v1/auth/session')).json()) as {
        ui?: { features?: string[] };
      };
      expect(ui.ui?.features ?? []).toContain('profile_browser');
    } finally {
      await api.dispose();
    }
  });
});

test('without the flag it is off again', async ({ playwright, state }) => {
  const api = await adminAPI(playwright, state);
  try {
    expect((await api.get(`api/v1/servers/${state.server_id}/profile`)).status()).toBe(404);
    const settings = (await (await api.get('api/v1/settings')).json()) as Record<string, unknown>;
    expect(settings['features.profile_browser']).toBe(false);
  } finally {
    await api.dispose();
  }
});
