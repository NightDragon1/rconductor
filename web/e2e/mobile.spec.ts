// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// The phone view (UI-012, Design 6.6): at 390 px no page scrolls sideways,
// every control is a tap target of 44 px, and the administration reads.

async function enter(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

// fits checks the whole page: no sideways scrolling, and every control
// that can be used measures at least 44 px each way.
async function fits(page: Page) {
  await page.waitForLoadState('networkidle');
  const r = await page.evaluate(() => {
    const small: string[] = [];
    const controls = 'a,button,input,select,textarea,summary,[role=menuitem]';
    for (const e of Array.from(document.querySelectorAll(controls))) {
      const box = e.getBoundingClientRect();
      const hidden = box.width === 0 || box.height === 0;
      const off = e.matches(':disabled') || e.getAttribute('aria-disabled') === 'true';
      if (hidden || off || e.matches('input[type=hidden]')) continue;
      if (box.width < 44 || box.height < 44) {
        const name = e.getAttribute('aria-label') ?? e.textContent ?? '';
        small.push(
          `${e.tagName} ${Math.round(box.width)}x${Math.round(box.height)} ${name.slice(0, 30)}`,
        );
      }
    }
    const d = document.documentElement;
    return { scroll: d.scrollWidth, width: d.clientWidth, small };
  });
  expect(r.scroll, 'sideways scrolling').toBeLessThanOrEqual(r.width);
  expect(r.small, 'tap targets below 44 px').toEqual([]);
}

test('login fits a phone (UI-012)', async ({ page, csp }) => {
  void csp;
  await page.goto('login');
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
  await fits(page);
});

for (const [user, tabs] of [
  ['admin', ['overview', 'console', 'logs', 'history']],
  ['sadmin', ['overview', 'console', 'logs', 'history']],
  ['mod', ['overview', 'commands', 'history']],
  ['sup', ['overview', 'commands', 'history']],
] as const) {
  test(`pages of ${user} fit a phone (UI-012)`, async ({ page, state, csp }) => {
    void csp;
    await enter(page, state, user);
    await fits(page);
    for (const path of [
      'servers',
      'profile',
      ...tabs.map((t) => `servers/${state.servers.survival}/${t}`),
    ]) {
      await page.goto(path);
      await fits(page);
    }
  });
}

test('the head links to the own profile, signing out stays in the menu', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await enter(page, state, 'mod');
  const banner = page.getByRole('banner');
  await expect(banner.getByRole('button', { name: 'Sign out' })).toHaveCount(0);
  await banner.getByRole('link', { name: 'Your profile' }).tap();
  await expect(page.getByRole('heading', { name: 'Your profile', level: 1 })).toBeVisible();
  await page.getByRole('button', { name: 'Menu' }).tap();
  const menu = page.getByRole('dialog', { name: 'Menu' });
  await menu.getByRole('button', { name: 'Sign out' }).tap();
  await expect(page).toHaveURL(/login/);
});

test('the console works by touch and hides the timeline (Design 6.5)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await enter(page, state, 'admin');
  await page.goto(`servers/${state.servers.survival}/console`);
  await expect(page.getByRole('navigation', { name: 'Timeline' })).toBeHidden();
  const input = page.getByRole('textbox', { name: 'Command' });
  await input.tap();
  await input.fill('whitelist');
  const chips = page.getByRole('list', { name: 'Suggestions' });
  await expect(chips).toBeVisible();
  await fits(page);
  await chips.getByRole('button').first().tap();
  await expect(input).toHaveValue(/^whitelist \S* ?$/);
  await input.fill('list');
  await page.getByRole('button', { name: 'Send' }).tap();
  const console_ = page.getByRole('region', { name: 'Console' });
  await expect(console_.getByRole('article', { name: 'list', exact: true }).last()).toBeVisible();
  await fits(page);
  // The input stays on the screen, however long the console is.
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect(input).toBeInViewport();
});

test('server actions sit in the menu and ask first (AGT-008)', async ({ page, state, csp }) => {
  void csp;
  await enter(page, state, 'admin');
  await page.goto(`servers/${state.servers.survival}/overview`);
  await expect(page.getByRole('group', { name: 'Server actions' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Server actions' }).tap();
  await expect(page.getByRole('menuitem')).toHaveText(['Start', 'Restart', 'Stop']);
  await fits(page);
  await page.getByRole('menuitem', { name: 'Stop' }).tap();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('survival');
  await dialog.getByRole('button', { name: 'Cancel' }).tap();
  await expect(dialog).toBeHidden();
  // Moderators get no actions (AGT-008).
  await page.context().clearCookies();
  await enter(page, state, 'mod');
  await page.goto(`servers/${state.servers.survival}/overview`);
  await expect(page.getByRole('heading', { name: 'survival', level: 1 })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Server actions' })).toHaveCount(0);
});

test('commands: one card open, players in the sheet (Design 6.6)', async ({ page, state, csp }) => {
  void csp;
  await enter(page, state, 'mod');
  await page.goto(`servers/${state.servers.survival}/commands`);
  const commands = page.getByRole('region', { name: 'Commands' });
  const card = (path: string) => commands.getByRole('form', { name: path, exact: true });
  await expect(card('list').getByRole('button', { name: 'Run' })).toBeVisible();
  // The first card with fields is open, the next opens in its place.
  await expect(card('kick').getByLabel('player', { exact: true })).toBeVisible();
  await expect(card('say').getByRole('button', { name: 'Run' })).toHaveCount(0);
  await card('say').getByRole('button', { expanded: false }).tap();
  await expect(card('say').getByRole('button', { name: 'Run' })).toBeVisible();
  await expect(card('kick').getByLabel('player', { exact: true })).toHaveCount(0);
  await card('kick').getByRole('button', { expanded: false }).tap();
  await fits(page);

  const sheet = page.getByRole('region', { name: 'Players online' });
  await expect(sheet).toBeInViewport();
  await card('kick').getByLabel('player', { exact: true }).tap();
  await sheet.getByRole('button', { name: 'Steve' }).tap();
  await expect(card('kick').getByLabel('player', { exact: true })).toHaveValue('Steve');
  await card('kick').getByRole('button', { name: 'Run' }).tap();
  await expect(page.getByRole('region', { name: 'My history' })).toContainText('kick Steve');

  await sheet.getByRole('button', { expanded: true }).tap();
  await expect(sheet.getByRole('button', { name: 'Steve' })).toHaveCount(0);
  await sheet.getByRole('button', { expanded: false }).tap();
  await expect(sheet.getByRole('button', { name: 'Steve' })).toBeVisible();
});

// What the administration may show on a phone; everything else that can be
// used would be a change.
const reading = /^(Close the details)$/;

async function readsOnly(page: Page, extra: RegExp = /^$/) {
  await page.waitForLoadState('networkidle');
  await expect(page.getByText('Read-only on a phone')).toBeVisible();
  const main = page.getByRole('main');
  for (const e of await main.getByRole('button').all()) {
    if (!(await e.isEnabled())) continue;
    const name = (await e.getAttribute('aria-label')) ?? (await e.textContent()) ?? '';
    const opens = (await e.locator('xpath=ancestor::td').count()) > 0;
    expect(opens || reading.test(name) || extra.test(name), `button "${name}"`).toBe(true);
  }
  await expect(main.getByRole('checkbox', { disabled: false })).toHaveCount(0);
  await expect(main.locator('input:not([type=search]):not(:disabled)')).toHaveCount(0);
  await fits(page);
}

test('the administration reads on a phone (UI-012)', async ({ page, state, csp }) => {
  void csp;
  await enter(page, state, 'admin');

  await page.goto('admin/groups');
  await readsOnly(page);
  await page.getByRole('button', { name: 'moderators', exact: true }).tap();
  const group = page.getByRole('complementary');
  await expect(group).toBeInViewport();
  await readsOnly(page);
  await expect(group.getByRole('button', { name: /Remove/ })).toHaveCount(0);
  await expect(group.getByRole('button', { name: /Delete/ })).toHaveCount(0);

  await page.goto('admin/command-lists');
  await readsOnly(page);
  await page.getByRole('button', { name: 'moderation', exact: true }).tap();
  await readsOnly(page);

  await page.goto('admin/servers');
  await expect(page.getByRole('button', { name: 'New server' })).toHaveCount(0);
  await readsOnly(page);
  await page.getByRole('button', { name: 'survival', exact: true }).tap();
  await readsOnly(page);
  await expect(page.getByRole('complementary').getByLabel('Name')).toBeDisabled();

  await page.goto('admin/agents');
  await expect(page.getByRole('button', { name: 'Pair an agent' })).toHaveCount(0);
  await readsOnly(page);
  await expect(page.getByRole('button', { name: 'Approve' })).toHaveCount(0);

  await page.goto('admin/settings');
  await readsOnly(page);
  await expect(page.getByRole('button', { name: /^Save/ })).toHaveCount(0);
});

async function userId(request: APIRequestContext, base: string, name: string) {
  const r = await request.get(`${base}api/v1/users`);
  const { items } = (await r.json()) as { items: { id: string; pseudonym: string }[] };
  const id = items.find((u) => u.pseudonym === name)?.id;
  if (!id) throw new Error(`no user ${name}`);
  return id;
}

test('users: search and details read; lock and end sessions ask (candidate 26)', async ({
  page,
  state,
  csp,
}) => {
  void csp;
  await enter(page, state, 'admin');
  await page.goto('admin/users');
  await expect(page.getByRole('heading', { name: 'Users', level: 1 })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Invite user' })).toHaveCount(0);
  await expect(page.getByRole('checkbox', { name: /Select/ })).toHaveCount(0);
  await page.getByRole('searchbox', { name: 'Search users' }).fill('sup');
  await expect(page.getByRole('button', { name: 'mod', exact: true })).toHaveCount(0);
  await fits(page);
  await page.getByRole('button', { name: 'sup', exact: true }).tap();
  const panel = page.getByRole('complementary');
  await expect(panel).toBeInViewport();
  await expect(panel.getByRole('combobox', { name: 'Role' })).toBeDisabled();
  for (const name of ['Reset password', 'Remove', 'Use as template']) {
    await expect(panel.getByRole('button', { name })).toHaveCount(0);
  }
  await fits(page);

  await panel.getByRole('button', { name: 'End sessions' }).tap();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('End the sessions of sup?');
  await dialog.getByRole('button', { name: 'End sessions' }).tap();
  await expect(panel.getByRole('status')).toContainText('All sessions ended.');

  const id = await userId(page.request, state.base_url, 'sup');
  try {
    await panel.getByRole('button', { name: 'Lock' }).tap();
    await expect(dialog).toContainText('Lock sup?');
    await dialog.getByRole('button', { name: 'Lock' }).tap();
    await expect(panel.getByRole('status')).toContainText('Saved');
    // Unlocking is no emergency: it stays on a larger screen.
    await expect(panel.getByRole('button', { name: 'Unlock' })).toHaveCount(0);
    await expect(panel.getByRole('button', { name: 'Lock' })).toHaveCount(0);
  } finally {
    const r = await page.request.patch(`${state.base_url}api/v1/users/${id}`, {
      headers: { Origin: new URL(state.base_url).origin },
      data: { disabled: false },
    });
    expect(r.ok()).toBe(true);
  }
});

test('a long server name fits a phone (SEC-006)', async ({ page, state, csp }) => {
  void csp;
  await enter(page, state, 'admin');
  const url = `${state.base_url}api/v1/servers/${state.servers.arena}`;
  // A change names the whole server and the version just read.
  const put = async (name: string) => {
    const now = (await (await page.request.get(url)).json()) as Record<string, unknown> & {
      version: string;
    };
    const r = await page.request.patch(url, {
      headers: { Origin: new URL(state.base_url).origin, 'If-Match': now.version },
      data: {
        name,
        profile_id: now.profile_id,
        host: now.host,
        port: now.port,
        agent_id: now.agent_id ?? '',
        container_name: now.container_name ?? '',
      },
    });
    expect(r.status(), await r.text()).toBe(200);
  };
  await put('x'.repeat(64));
  try {
    await page.goto('');
    await fits(page);
    await page.goto(`servers/${state.servers.arena}/console`);
    await fits(page);
  } finally {
    await put('arena');
  }
});
