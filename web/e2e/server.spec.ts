// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

const tabs = (page: Page) =>
  page.getByRole('navigation', { name: 'Server tabs' }).getByRole('link');
const actions = (page: Page) => page.getByRole('group', { name: 'Server actions' });

async function open(page: Page, state: StackState, user: string, server: string, tab = '') {
  await page.context().clearCookies();
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers[server]}${tab ? `/${tab}` : ''}`);
}

test('tabs and start tab per role (UI-011, Design 6.3)', async ({ page, state }) => {
  const want: [string, string, string[], string][] = [
    ['admin', 'survival', ['Overview', 'Console', 'Logs', 'History'], 'console'],
    ['admin', 'outpost', ['Overview', 'Console', 'History'], 'console'],
    ['sadmin', 'survival', ['Overview', 'Console', 'Logs', 'History'], 'console'],
    ['mod', 'survival', ['Overview', 'Commands', 'History'], 'commands'],
    ['sup', 'survival', ['Overview', 'Commands', 'History'], 'commands'],
  ];
  for (const [user, server, names, start] of want) {
    await open(page, state, user, server);
    await expect(page).toHaveURL(new RegExp(`servers/${state.servers[server]}/${start}$`));
    await expect(tabs(page)).toHaveText(names);
    await expect(page.getByRole('heading', { name: server, level: 1 })).toBeVisible();
  }
});

test('a tab or server outside the role does not exist', async ({ page, state }) => {
  const denied: [string, string, string][] = [
    ['mod', 'survival', 'console'],
    ['mod', 'survival', 'logs'],
    ['sup', 'survival', 'console'],
    ['admin', 'survival', 'commands'],
    ['sadmin', 'survival', 'commands'],
  ];
  for (const [user, server, tab] of denied) {
    await page.context().clearCookies();
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    const requests: string[] = [];
    page.on('request', (r) => {
      const u = new URL(r.url());
      if (u.pathname.includes('/api/v1/') && !u.pathname.endsWith('/api/v1/auth/session')) {
        requests.push(u.pathname);
      }
    });
    await page.goto(`servers/${state.servers[server]}/${tab}`);
    await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
    await page.waitForLoadState('networkidle');
    expect(requests, `${user} ${tab}`).toEqual([]);
    page.removeAllListeners('request');
  }
  // Logs without an agent have no source (F11).
  await open(page, state, 'admin', 'outpost', 'logs');
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  // A server outside the scope looks like none (Design 3.1).
  await open(page, state, 'mod', 'arena');
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  await open(page, state, 'sadmin', 'outpost');
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
});

test('actions only for admins in scope (AGT-008)', async ({ page, state }) => {
  for (const user of ['mod', 'sup']) {
    await open(page, state, user, 'survival', 'overview');
    await expect(page.getByRole('heading', { name: 'survival', level: 1 })).toBeVisible();
    await expect(actions(page)).toHaveCount(0);
    await expect(page.getByRole('button', { name: /Restart|Stop|Start/ })).toHaveCount(0);
  }
  await open(page, state, 'sadmin', 'survival', 'overview');
  await expect(actions(page).getByRole('button')).toHaveText(['Start', 'Restart', 'Stop']);
  await expect(actions(page).getByRole('button', { name: 'Start', exact: true })).toBeDisabled();
  // Stop asks first; cancelling sends nothing.
  await actions(page).getByRole('button', { name: 'Stop', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Stop survival?');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  // A restart goes to the agent.
  await actions(page).getByRole('button', { name: 'Restart', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Restart', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Restart sent.');
});

test('without agent only the restart over RCON (AGT-009, W6)', async ({ page, state }) => {
  await open(page, state, 'admin', 'outpost', 'overview');
  await expect(actions(page).getByRole('button')).toHaveText(['Restart over RCON']);
  await actions(page).getByRole('button', { name: 'Restart over RCON' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('stopped with its stop command');
  await expect(dialog).toContainText('on-failure does not apply to a clean shutdown');
  await dialog.getByRole('button', { name: 'Restart over RCON' }).click();
  await expect(page.getByRole('status')).toHaveText('Stop command sent.');
});

test('overview tab with players and load', async ({ page, state }) => {
  await open(page, state, 'admin', 'survival', 'overview');
  const players = page.getByRole('region', { name: 'Players online' });
  // Player names are foreign data and stay text (SEC-006).
  await expect(players.getByRole('listitem')).toHaveText(['Steve', '<b>Alex</b>']);
  const load = page.getByRole('region', { name: 'Load' });
  await expect(load).toContainText('38 %');
  await expect(load).toContainText('5.0 / 8.0 GB');
  await open(page, state, 'mod', 'survival', 'overview');
  await expect(page.getByRole('region', { name: 'Players online' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Load' })).toHaveCount(0);
  await expect(page.getByRole('main')).not.toContainText('CPU');
});

test('history by role, with the switch for own commands (ROLE-009)', async ({ page, state }) => {
  await open(page, state, 'admin', 'survival', 'history');
  const history = page.getByRole('region', { name: 'History' });
  await expect(history).toContainText('say hi from mod');
  await expect(history).toContainText('list');
  const colored = history.getByRole('listitem').filter({ hasText: 'say §ahello' });
  await colored.getByText('Answer', { exact: true }).click();
  // Color codes of the answer are dropped, never styled (W1).
  await expect(colored.locator('pre')).toHaveText('Done: say hello world');
  // Moderators share a pool on their servers, admin commands stay hidden.
  await open(page, state, 'mod2', 'survival', 'history');
  const pool = page.getByRole('region', { name: 'History' });
  await expect(pool).toContainText('say hi from mod');
  await expect(pool).not.toContainText('hello');
  await pool.getByRole('checkbox', { name: 'Only my commands' }).check();
  await expect(pool).not.toContainText('say hi from mod');
  await open(page, state, 'mod', 'survival', 'history');
  const own = page.getByRole('region', { name: 'History' });
  await own.getByRole('checkbox', { name: 'Only my commands' }).check();
  await expect(own).toContainText('say hi from mod');
  await expect(own).not.toContainText('hello');
});

test('server list with search (F13)', async ({ page, state }) => {
  await login(page, 'mod', secret(state, 'mod'));
  await page.getByRole('link', { name: 'Servers', exact: true }).first().click();
  await expect(page.getByRole('heading', { name: 'Servers', level: 1 })).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(1);
  await page.getByRole('article', { name: 'survival' }).getByRole('link').click();
  await expect(page).toHaveURL(new RegExp(`servers/${state.servers.survival}/commands$`));
});

test('an older agent hides what it cannot do (AGT-013)', async ({ browser, page, state }) => {
  // A server of its own on host-legacy, which reports only restart_info.
  const admin = await browser.newContext({ baseURL: state.base_url });
  const adminPage = await admin.newPage();
  await login(adminPage, 'admin', secret(state, 'admin'));
  await expect(adminPage.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const headers = { Origin: new URL(state.base_url).origin };
  const outpost = await (
    await adminPage.request.get(`api/v1/servers/${state.servers.outpost}`)
  ).json();
  const created = await adminPage.request.post('api/v1/servers', {
    data: {
      name: 'legacy',
      profile_ids: outpost.profile_ids,
      host: outpost.host,
      port: outpost.port,
      password: 'e2e-rcon',
      agent_id: state.legacy_agent_id,
      container_name: 'mc-legacy',
    },
    headers,
  });
  expect(created.status()).toBe(201);
  const id = (await created.json()).id as string;
  try {
    await page.context().clearCookies();
    await login(page, 'admin', secret(state, 'admin'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto(`servers/${id}`);
    await expect(page.getByRole('heading', { name: 'legacy', level: 1 })).toBeVisible();
    await expect(
      page.getByText('The agent does not support actions. Update the agent.'),
    ).toBeVisible();
    await expect(actions(page)).toHaveCount(0);
    await expect(tabs(page)).toHaveText(['Overview', 'Console', 'History']);
    // Its metrics are dropped: no CPU in the head.
    const head = page.getByRole('region', { name: 'legacy' });
    await expect(head).toContainText('host-legacy');
    await expect(head.getByText('CPU')).toHaveCount(0);
    await expect(head.getByText('Version')).toHaveCount(0);
    // The backend refuses on its own.
    const r = await adminPage.request.post(`api/v1/servers/${id}/actions`, {
      data: { action: 'restart' },
      headers,
    });
    expect(r.status()).toBe(409);
    expect((await r.json()).reason).toBe('agent_unsupported');
  } finally {
    const d = await adminPage.request.delete(`api/v1/servers/${id}`, { headers });
    expect(d.status()).toBe(204);
    await admin.close();
  }
});

test('the game version is a server datum for every role (AGT-006, UI-006)', async ({
  page,
  state,
}) => {
  for (const user of ['admin', 'mod']) {
    await open(page, state, user, 'survival');
    const head = page.getByRole('region', { name: 'survival' });
    await expect(head).toContainText('Version');
    await expect(head).toContainText('26.3');
  }
});
