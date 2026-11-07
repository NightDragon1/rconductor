// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

test('container logs live, earlier lines, filter (UI-009)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const topic = `server.${state.servers.survival}.logs`;
  const sent: string[] = [];
  page.on('websocket', (ws) => ws.on('framesent', (f) => sent.push(String(f.payload))));
  await page.goto(`servers/${state.servers.survival}/logs`);
  const lines = page.getByRole('region', { name: 'Logs' }).locator('pre');
  // The ring of the panel first; color codes are dropped (W1).
  await expect(lines).toContainText('Steve joined the game');
  await expect(lines).toContainText('Done (1.2s)!');
  await expect(lines).not.toContainText('§');
  // New lines arrive while the tab is open.
  await expect(lines).toContainText(/tick \d+/, { timeout: 10_000 });
  expect(sent.some((m) => m.includes('"subscribe"') && m.includes(topic))).toBe(true);

  // Earlier lines over logs.tail, without the lines the view holds.
  await page.getByRole('button', { name: 'Load earlier lines' }).click();
  await expect(lines).toContainText('Starting minecraft server version 26.3');
  const text = (await lines.textContent()) ?? '';
  expect(text.indexOf('Preparing level')).toBeLessThan(text.indexOf('Steve joined'));
  expect(text.split('Steve joined the game')).toHaveLength(2);
  await expect(page.getByRole('button', { name: 'Load earlier lines' })).toHaveCount(0);

  await page.getByRole('searchbox', { name: 'Filter the logs' }).fill('tick');
  await expect(lines).not.toContainText('Steve');
  await expect(lines).toContainText('tick');

  // Leaving the tab ends the subscription, and with the last one the
  // agent's logs.follow (UI-009).
  await page
    .getByRole('navigation', { name: 'Server tabs' })
    .getByRole('link', { name: 'Overview' })
    .click();
  await expect
    .poll(() => sent.some((m) => m.includes('"unsubscribe"') && m.includes(topic)))
    .toBe(true);
});

test('follow, wrap and clear', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/logs`);
  const region = page.getByRole('region', { name: 'Logs' });
  const follow = region.getByRole('button', { name: 'Follow' });
  await expect(follow).toHaveAttribute('aria-pressed', 'true');
  await follow.click();
  await expect(follow).toHaveAttribute('aria-pressed', 'false');
  const wrap = region.getByRole('button', { name: 'Wrap lines' });
  await wrap.click();
  await expect(region.locator('pre')).toHaveClass(/whitespace-pre(?!-)/);
  await region.getByRole('button', { name: 'Clear view' }).click();
  await expect(region).toContainText('No log lines yet.');
});
