// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// O6: host, RCON port and game ports per role on the pages with server
// data, and in what the backend sends to the page (UI-005, UI-006, D-056).
// The interface only hides; the answers themselves must not carry what a
// role may not see.

// record collects the JSON bodies of the API and the WebSocket frames the
// page receives. The game profiles are left out: they are the shipped
// profile files with their default ports, the same for every role, and
// hold no data of a server.
function record(page: Page): string[] {
  const seen: string[] = [];
  page.on('response', async (r) => {
    const path = new URL(r.url()).pathname;
    if (!path.includes('/api/v1/') || path.endsWith('/api/v1/profiles')) return;
    if (!(r.headers()['content-type'] ?? '').includes('json')) return;
    try {
      seen.push(await r.text());
    } catch {
      // the page navigated away before the body was read
    }
  });
  page.on('websocket', (ws) => ws.on('framereceived', (f) => seen.push(String(f.payload))));
  return seen;
}

async function signIn(page: Page, state: StackState, user: string) {
  await page.context().clearCookies();
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

test('O6 host and ports per role on the server pages (UI-005, UI-006)', async ({ page, state }) => {
  const survival = state.servers.survival;
  await signIn(page, state, 'admin');
  const conn = (await (await page.request.get(`api/v1/servers/${survival}`)).json()) as {
    host: string;
    port: number;
  };
  expect(conn.host).toBeTruthy();
  expect(conn.port).toBeGreaterThan(0);

  // RCON admin: host and RCON port on Servers and groups, every port on
  // the server.
  await page.goto('admin/servers');
  await expect(page.getByRole('heading', { name: 'Servers and groups', level: 1 })).toBeVisible();
  await expect(page.getByRole('row', { name: /^survival / })).toContainText(
    `${conn.host}:${conn.port}`,
  );
  await page.goto(`servers/${survival}/overview`);
  const adminPorts = page.getByRole('region', { name: 'Ports' }).getByRole('listitem');
  await expect(adminPorts.filter({ hasText: 'game' })).not.toHaveCount(0);
  await expect(adminPorts.filter({ hasText: 'RCON' })).toHaveCount(1);

  // Server admin: the game ports; neither host nor RCON port on the pages
  // or in what they receive (D-056).
  {
    const seen = record(page);
    await signIn(page, state, 'sadmin');
    await page.goto(`servers/${survival}/overview`);
    const ports = page.getByRole('region', { name: 'Ports' }).getByRole('listitem');
    await expect(ports.filter({ hasText: 'game' })).not.toHaveCount(0);
    await expect(ports.filter({ hasText: 'RCON' })).toHaveCount(0);
    await page.goto('');
    await expect(page.getByRole('article', { name: 'survival', exact: true })).toBeVisible();
    await page.goto('admin/servers');
    await expect(page.getByRole('heading', { name: 'Servers and groups', level: 1 })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Address' })).toHaveCount(0);
    await page.getByRole('button', { name: 'survival', exact: true }).click();
    const panel = page.getByRole('complementary', { name: 'Details of survival' });
    await expect(panel.getByLabel('Name')).toBeVisible();
    await expect(panel.getByLabel('Host')).toHaveCount(0);
    await expect(page.getByRole('main')).not.toContainText(conn.host);
    await page.waitForLoadState('networkidle');
    expect(seen.length).toBeGreaterThan(0);
    for (const body of seen) {
      expect(body, 'server admin').not.toMatch(/"kind":"(rcon|other)"/);
      // Game ports carry a port too; the RCON port must not appear.
      expect(body, 'server admin').not.toMatch(/"host":/);
      expect(body, 'server admin').not.toMatch(
        new RegExp(`"(port|private|public)":${conn.port}\\b`),
      );
      expect(body, 'server admin').not.toContain(conn.host);
    }
    page.removeAllListeners('response');
    page.removeAllListeners('websocket');
  }

  // Moderator and supporter: no Servers and groups page, no ports, and
  // neither host nor any port in what the pages receive.
  for (const user of ['mod', 'sup']) {
    const seen = record(page);
    await signIn(page, state, user);
    await expect(page.getByRole('article', { name: 'survival', exact: true })).toBeVisible();
    await page.goto(`servers/${survival}/overview`);
    await expect(page.getByRole('region', { name: 'Players online' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Ports' })).toHaveCount(0);
    await page.goto('servers');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await page.goto('admin/servers');
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
    await page.waitForLoadState('networkidle');
    expect(seen.length).toBeGreaterThan(0);
    for (const body of seen) {
      expect(body, user).not.toMatch(
        /"(host|port|protocol|game_ports|ports|agent_id|container_name)":/,
      );
      expect(body, user).not.toContain(conn.host);
    }
    page.removeAllListeners('response');
    page.removeAllListeners('websocket');
  }
});
