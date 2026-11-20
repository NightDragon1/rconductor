// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Locator, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Docker host of the agents (AGT-014) behind the flag docker_host: the fake
// daemon of host-1 names its host "<b>nas</b>-1" with a direction override
// and the address 192.168.1.5 (internal/e2e/stack); the agent host-legacy
// sends none, as an agent of 1.1.

const hostName = '<b>nas</b>-1';
const hostIP = '192.168.1.5';

async function as(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

// agentNode is the line of an agent in the topology.
const agentNode = (page: Page, agent: string) =>
  page
    .getByRole('region', { name: 'minecraft' })
    .getByRole('listitem')
    .filter({ hasText: agent })
    .first();

// connectedFrom: the connection address shows, marked, exactly when the
// agent has no host address and the backend sends one; it does so only
// outside the panel's networks (AGT-014, D-084).
async function connectedFrom(at: Locator, a?: { remote?: string; docker_host?: { ip?: string } }) {
  const remote = a?.docker_host?.ip ? undefined : a?.remote;
  if (remote) await expect(at).toContainText(`Connected from ${remote}`);
  else await expect(at).not.toContainText('Connected from');
}

test.describe('docker host on', () => {
  test.use({ flags: ['docker_host'] });

  test('the RCON admin sees the Docker host as text in topology and agent list', async ({
    page,
    state,
  }) => {
    await as(page, state, 'admin');
    const r = await page.request.get('api/v1/agents');
    expect(r.status()).toBe(200);
    const { items } = (await r.json()) as {
      items: { name: string; remote?: string; docker_host?: { name?: string; ip?: string } }[];
    };
    const host1 = items.find((a) => a.name === 'host-1');
    expect(host1?.docker_host).toEqual({ name: hostName, ip: hostIP });
    expect(items.find((a) => a.name === 'host-legacy')?.docker_host).toBeUndefined();

    await page.goto('topology');
    const node = agentNode(page, 'host-1');
    await expect(node).toContainText(`${hostName} (${hostIP})`);
    // Markup stays text (SEC-006); a connection address only marked as
    // such, and only when the backend sends one (D-084).
    await expect(node.locator('b')).toHaveCount(0);
    await connectedFrom(node, host1);

    // The server page names the host of its agent (AGT-014: each server with agent).
    await page.goto(`servers/${state.servers.survival}/overview`);
    const head = page.getByRole('region', { name: 'survival', exact: true });
    await expect(head).toContainText(`Docker host ${hostName} (${hostIP})`);
    await expect(head.locator('b')).toHaveCount(0);

    await page.goto('admin/agents');
    const row = page.getByRole('row').filter({ hasText: 'host-1' });
    await expect(page.getByRole('columnheader', { name: 'Docker host' })).toBeVisible();
    await expect(row).toContainText(`${hostName} (${hostIP})`);
    await expect(row.locator('b')).toHaveCount(0);
    await connectedFrom(row, host1);
    const legacy = items.find((a) => a.name === 'host-legacy');
    await connectedFrom(page.getByRole('row').filter({ hasText: 'host-legacy' }), legacy);
  });

  test('a server admin gets neither name nor address', async ({ page, state }) => {
    await as(page, state, 'sadmin');
    expect((await page.request.get('api/v1/agents')).status()).toBe(403);
    await page.goto('topology');
    await expect(page.getByRole('link', { name: 'survival' })).toBeVisible();
    await expect(page.getByRole('main')).toContainText('Agent');
    let html = await page.content();
    expect(html).not.toContain(hostIP);
    expect(html).not.toContain('nas</b>');
    expect(html).not.toContain('Connected from');
    await page.goto(`servers/${state.servers.survival}/overview`);
    await expect(page.getByRole('region', { name: 'survival', exact: true })).toBeVisible();
    html = await page.content();
    expect(html).not.toContain(hostIP);
    expect(html).not.toContain('Docker host');
  });
});

test.describe('flag off', () => {
  test.use({ flagsOff: ['docker_host'] });

  test('docker host off: the agent data are as in 1.1', async ({ page, state }) => {
    await as(page, state, 'admin');
    const r = await page.request.get('api/v1/agents');
    const { items } = (await r.json()) as { items: { docker_host?: unknown }[] };
    expect(items.every((a) => a.docker_host === undefined)).toBe(true);
    await page.goto('topology');
    await expect(agentNode(page, 'host-1')).not.toContainText(hostIP);
    await page.goto('admin/agents');
    await expect(page.getByRole('row').filter({ hasText: 'host-1' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Docker host' })).toHaveCount(0);
  });
});
