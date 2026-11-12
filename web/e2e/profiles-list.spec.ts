// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequestContext, Locator, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// The ordered profile list of a server (SRV-008, CAT-007, D-074): add with
// "+", remove with "−", reorder by drag and drop; only profiles of the
// list's protocol are offered, a new specific profile goes above the
// generic ones, and a generic one above a specific one shows a hint.

const java = 'Minecraft (Java Edition)';
const paper = 'Minecraft Paper (Spigot/Bukkit)';
// A specific profile shows with its kind; its remove button names it bare.
const essentials = 'EssentialsX (specific)';
const httpApi = 'Satisfactory';
const hint = 'A generic profile stands above a specific one; the specific one should be on top.';
const saved = { timeout: 9000 };

// admin opens an API session of the RCON admin, for setup and cleanup.
async function admin(
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

async function profileId(api: APIRequestContext, slug: string): Promise<string> {
  const r = await api.get('api/v1/profiles');
  const items = ((await r.json()) as { items: { id: string; slug: string }[] }).items;
  const p = items.find((x) => x.slug === slug);
  if (!p) throw new Error(`no profile ${slug}`);
  return p.id;
}

// setProfiles writes the profile list of a server over the API.
async function setProfiles(api: APIRequestContext, id: string, ids: string[]) {
  const version = ((await (await api.get(`api/v1/servers/${id}`)).json()) as { version: string })
    .version;
  const r = await api.patch(`api/v1/servers/${id}`, {
    data: { profile_ids: ids },
    headers: { 'If-Match': version },
  });
  expect(r.status()).toBe(200);
}

async function savedIds(api: APIRequestContext, id: string): Promise<string[]> {
  return ((await (await api.get(`api/v1/servers/${id}`)).json()) as { profile_ids: string[] })
    .profile_ids;
}

// open shows the details of a server on the admin page.
async function open(page: Page, name: string): Promise<Locator> {
  await expect(page.getByRole('heading', { name: 'Servers and groups', level: 1 })).toBeVisible();
  await page.getByRole('button', { name, exact: true }).click();
  const panel = page.getByRole('complementary', { name: `Details of ${name}` });
  await expect(panel.getByTestId('profile-item').first()).toBeVisible();
  return panel;
}

async function save(panel: Locator) {
  await panel.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(panel.getByRole('status').filter({ hasText: 'Saved.' })).toBeVisible(saved);
}

// reopen reloads the page and shows the server again: what it lists is
// what the backend stored.
async function reopen(page: Page, name: string): Promise<Locator> {
  await page.reload();
  return open(page, name);
}

const options = (panel: Locator) => panel.getByLabel('Add a game profile').locator('option');

test('RCON admin adds, removes and reorders the profiles of a server (SRV-008, CAT-007)', async ({
  page,
  playwright,
  state,
}) => {
  const api = await admin(playwright, state);
  const name = `e2e-profiles-${test.info().project.name}`;
  const created = await api.post('api/v1/servers', {
    data: {
      name,
      profile_ids: [await profileId(api, 'minecraft-java')],
      host: 'mc.example',
      port: 25575,
      password: 'a secret rcon password',
    },
  });
  expect(created.status()).toBe(201);
  const id = ((await created.json()) as { id: string }).id;
  try {
    await login(page, 'admin', secret(state, 'admin'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto('admin/servers');
    let panel = await open(page, name);
    const items = panel.getByTestId('profile-item');
    await expect(items).toHaveText([java]);

    // Only profiles of the server's protocol (RCON) are offered.
    for (const label of [paper, essentials]) {
      await expect(options(panel).filter({ hasText: label })).toHaveCount(1);
    }
    await expect(options(panel).filter({ hasText: httpApi })).toHaveCount(0);
    await expect(options(panel).filter({ hasText: java })).toHaveCount(0);

    // "+": a generic profile goes to the end, a specific one above the
    // generic ones.
    await panel.getByLabel('Add a game profile').selectOption({ label: paper });
    await expect(items).toHaveText([java, paper]);
    await panel.getByLabel('Add a game profile').selectOption({ label: essentials });
    await expect(items).toHaveText([essentials, java, paper]);
    await expect(panel.getByText(hint)).toHaveCount(0);
    await save(panel);
    panel = await reopen(page, name);
    await expect(panel.getByTestId('profile-item')).toHaveText([essentials, java, paper]);

    // Drag and drop: Paper onto Java swaps the two generic profiles.
    let list = panel.getByTestId('profile-item');
    await list.nth(2).dragTo(list.nth(1));
    await expect(list).toHaveText([essentials, paper, java]);
    await save(panel);
    panel = await reopen(page, name);
    await expect(panel.getByTestId('profile-item')).toHaveText([essentials, paper, java]);

    // The specific profile dragged below a generic one: the hint shows and
    // the order the user chose is kept.
    list = panel.getByTestId('profile-item');
    await list.nth(0).dragTo(list.nth(2));
    await expect(list).toHaveText([paper, java, essentials]);
    await expect(panel.getByText(hint)).toBeVisible();
    await save(panel);
    panel = await reopen(page, name);
    await expect(panel.getByTestId('profile-item')).toHaveText([paper, java, essentials]);
    await expect(panel.getByText(hint)).toBeVisible();

    // "−" removes one; the hint goes once the order is right again.
    await panel.getByRole('button', { name: `Remove ${java}`, exact: true }).click();
    await expect(panel.getByTestId('profile-item')).toHaveText([paper, essentials]);
    await panel.getByRole('button', { name: `Remove ${paper}`, exact: true }).click();
    await expect(panel.getByTestId('profile-item')).toHaveText([essentials]);
    await expect(panel.getByText(hint)).toHaveCount(0);
    await save(panel);
    panel = await reopen(page, name);
    await expect(panel.getByTestId('profile-item')).toHaveText([essentials]);
    expect(await savedIds(api, id)).toEqual([await profileId(api, 'minecraft-essentialsx')]);
  } finally {
    expect((await api.delete(`api/v1/servers/${id}`)).status()).toBeLessThan(300);
    await api.dispose();
  }
});

test('the server admin in scope edits the profile list of his server (SRV-008, CAT-007)', async ({
  page,
  playwright,
  state,
}) => {
  const api = await admin(playwright, state);
  const before = await savedIds(api, state.server_id);
  try {
    await login(page, 'sadmin', secret(state, 'sadmin'));
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await page.goto('admin/servers');
    let panel = await open(page, state.server_name);
    await expect(panel.getByTestId('profile-item')).toHaveText([java]);
    await expect(options(panel).filter({ hasText: httpApi })).toHaveCount(0);

    await panel.getByLabel('Add a game profile').selectOption({ label: essentials });
    await expect(panel.getByTestId('profile-item')).toHaveText([essentials, java]);
    await save(panel);
    panel = await reopen(page, state.server_name);
    const list = panel.getByTestId('profile-item');
    await expect(list).toHaveText([essentials, java]);

    await list.nth(1).dragTo(list.nth(0));
    await expect(list).toHaveText([java, essentials]);
    await expect(panel.getByText(hint)).toBeVisible();
    await save(panel);
    panel = await reopen(page, state.server_name);
    await expect(panel.getByTestId('profile-item')).toHaveText([java, essentials]);
    await expect(panel.getByText(hint)).toBeVisible();

    await panel.getByRole('button', { name: 'Remove EssentialsX', exact: true }).click();
    await expect(panel.getByTestId('profile-item')).toHaveText([java]);
    await save(panel);
    panel = await reopen(page, state.server_name);
    await expect(panel.getByTestId('profile-item')).toHaveText([java]);
    expect(await savedIds(api, state.server_id)).toEqual(before);
  } finally {
    // The seeded server keeps its list for the other specs.
    if ((await savedIds(api, state.server_id)).join() !== before.join()) {
      await setProfiles(api, state.server_id, before);
    }
    await api.dispose();
  }
});
