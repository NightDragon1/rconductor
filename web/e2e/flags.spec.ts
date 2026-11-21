// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext } from '@playwright/test';
import { test, expect, secret, type StackState } from './fixtures';

// The flags are on by default (NFR-019); a test that names a flag in
// flagsOff gets it off through the settings API, and it is on again after
// the test.

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

test.describe('with the flag off', () => {
  test.use({ flagsOff: ['profile_browser'] });

  test('the guarded endpoint answers 404', async ({ playwright, state }) => {
    const api = await adminAPI(playwright, state);
    try {
      expect((await api.get(`api/v1/servers/${state.server_id}/profile`)).status()).toBe(404);
      const ui = (await (await api.get('api/v1/auth/session')).json()) as {
        ui?: { features?: string[] };
      };
      expect(ui.ui?.features ?? []).not.toContain('profile_browser');
    } finally {
      await api.dispose();
    }
  });
});

test('afterwards it is on again, as by default', async ({ playwright, state }) => {
  const api = await adminAPI(playwright, state);
  try {
    expect((await api.get(`api/v1/servers/${state.server_id}/profile`)).status()).toBe(200);
    const settings = (await (await api.get('api/v1/settings')).json()) as Record<string, unknown>;
    const flags = Object.entries(settings).filter(([k]) => k.startsWith('features.'));
    expect(flags.length).toBeGreaterThan(0);
    for (const [k, v] of flags) expect(v, k).toBe(true);
  } finally {
    await api.dispose();
  }
});
