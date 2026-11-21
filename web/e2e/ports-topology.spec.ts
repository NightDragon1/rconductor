// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// survival runs on host-1 with the game port 25565 and the map port 8100
// published and the volume mc-survival-data (internal/e2e/stack).

test('ports and volumes per role (UI-005)', async ({ page, state }) => {
  const want: [string, string[]][] = [
    ['admin', ['game25565/tcp', 'RCON', 'port8100/tcp']],
    ['sadmin', ['game25565/tcp']],
  ];
  for (const [user, chips] of want) {
    await page.context().clearCookies();
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(`servers/${state.servers.survival}/overview`);
    const ports = page.getByRole('region', { name: 'Ports' }).getByRole('listitem');
    await expect(ports).toHaveCount(chips.length);
    for (const [i, chip] of chips.entries()) await expect(ports.nth(i)).toContainText(chip);
    const volumes = page.getByRole('region', { name: 'Volumes' });
    await expect(volumes).toContainText('mc-survival-data');
    await expect(volumes).toContainText(/7\.0 GB, measured/);
  }
  // The card carries the game ports only.
  await page.goto('');
  const card = page.getByRole('article', { name: 'survival', exact: true });
  await expect(card.getByRole('list', { name: 'Ports' }).getByRole('listitem')).toHaveCount(1);

  for (const user of ['mod', 'sup']) {
    await page.context().clearCookies();
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(`servers/${state.servers.survival}/overview`);
    await expect(page.getByRole('region', { name: 'Players online' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Ports' })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Volumes' })).toHaveCount(0);
  }
});

test('topology per role, live without reload (UI-010)', async ({ page, state, browser }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('topology');
  // Panel → host → server from left to right (UI-010).
  await expect(page.getByRole('main')).toContainText('Panel');
  const hosts = page.getByRole('list', { name: 'Hosts' });
  const host1 = hosts.getByRole('link').filter({ hasText: 'host-1' });
  await expect(host1).toContainText('connected');
  await expect(host1).not.toContainText('Major version');
  await expect(hosts.getByRole('link', { name: 'survival' })).toBeVisible();
  const rest = hosts.getByRole('listitem').filter({ hasText: 'Without agent' });
  const outpost = rest.getByRole('listitem').filter({ hasText: /^outpost/ });
  await expect(outpost).toContainText('online');

  // Another admin moves outpost to a closed port: the page follows.
  const other = await browser.newContext({ baseURL: state.base_url });
  const req = other.request;
  const headers = { Origin: new URL(state.base_url).origin };
  expect(
    (
      await req.post('api/v1/auth/login', {
        data: { pseudonym: 'admin', password: secret(state, 'admin') },
        headers,
      })
    ).status(),
  ).toBe(200);
  const before = await (await req.get(`api/v1/servers/${state.servers.outpost}`)).json();
  const patch = async (port: number, version: string) => {
    const r = await req.patch(`api/v1/servers/${state.servers.outpost}`, {
      data: { port },
      headers: { ...headers, 'If-Match': version },
    });
    expect(r.status()).toBe(200);
    return (await r.json()).version as string;
  };
  const changed = await patch(1, before.version);
  try {
    await expect(outpost).toContainText('offline', { timeout: 15_000 });
  } finally {
    await patch(before.port, changed);
    await other.close();
  }
  await expect(outpost).toContainText('online', { timeout: 15_000 });

  // The server admin sees the agent without its name (F10).
  await page.context().clearCookies();
  await login(page, 'sadmin', secret(state, 'sadmin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('topology');
  await expect(page.getByRole('link', { name: 'survival' })).toBeVisible();
  await expect(page.getByRole('list', { name: 'Hosts' })).toContainText('connected');
  await expect(page.getByRole('main')).not.toContainText('host-1');
  // No link to the agents for him.
  await expect(page.locator('a[href*="admin/agents"]')).toHaveCount(0);
});

// UI-010: a host opens its agent, a server its page.
test('the topology opens agent and server (UI-010)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('topology');
  const hosts = page.getByRole('list', { name: 'Hosts' });
  await hosts.getByRole('link').filter({ hasText: 'host-1' }).click();
  await expect(page.getByRole('heading', { name: 'Agents', level: 1 })).toBeVisible();
  await expect(page).toHaveURL(/admin\/agents#/);
  await page.goBack();
  await hosts.getByRole('link', { name: 'survival' }).click();
  await expect(page).toHaveURL(new RegExp(`servers/${state.servers.survival}`));
});

// O5: the cards on the server list carry the game ports for the roles that
// may see them (D-056): the RCON admin on every server, the server admin on
// those in his scope, the moderator and the supporter none. These pass on
// the code before them as well: in this stack the cards showed the game
// ports already.
for (const [user, servers] of [
  ['admin', ['survival', 'outpost', 'lobby']],
  ['sadmin', ['survival']],
] as const) {
  test(`O5: the server cards show the game ports to ${user} (UI-004, UI-005)`, async ({
    page,
    state,
  }) => {
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.getByRole('navigation').getByRole('link', { name: 'Servers', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Servers', level: 1 })).toBeVisible();
    for (const name of servers) {
      const chips = page
        .getByRole('article', { name, exact: true })
        .getByRole('list', { name: 'Ports' })
        .getByRole('listitem');
      // Game ports only: no RCON port and no other port on a card.
      await expect(chips).toHaveCount(1);
      await expect(chips.first()).toContainText('game25565/tcp');
    }
    // survival publishes its game port: no "not published" mark.
    await expect(
      page.getByRole('article', { name: 'survival', exact: true }).getByRole('listitem'),
    ).not.toContainText('not published');
  });
}

for (const user of ['mod', 'sup']) {
  test(`O5: the server cards show no ports to ${user} (UI-004, UI-005)`, async ({
    page,
    state,
  }) => {
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.getByRole('navigation').getByRole('link', { name: 'Servers', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Servers', level: 1 })).toBeVisible();
    const card = page.getByRole('article', { name: 'survival', exact: true });
    await expect(card).toBeVisible();
    await expect(card.getByRole('list', { name: 'Ports' })).toHaveCount(0);
    await expect(card).not.toContainText('25565');
  });
}
