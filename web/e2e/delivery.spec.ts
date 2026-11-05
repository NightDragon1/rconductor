// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// Issue #88 (NFR-003): reloading a deep link loads every asset, also below
// the base path; SEC-009: the headers are on every answer and the page runs
// without a policy violation.
test('a deep link loads after a reload', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  const failed: string[] = [];
  page.on('response', (r) => {
    if (r.status() >= 400) failed.push(`${r.status()} ${r.url()}`);
  });
  const deep = await page.goto('no/such/page');
  expect(deep?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  expect(failed).toEqual([]);
  await page.getByRole('link', { name: 'Back to the overview' }).click();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
});

test('every answer carries the security headers', async ({ page, request, state }) => {
  const ui = await page.goto('./');
  const csp = ui?.headers()['content-security-policy'] ?? '';
  expect(csp).toContain("script-src 'self'");
  expect(csp).not.toContain('unsafe-inline');
  expect(csp).toContain("frame-ancestors 'none'");
  const api = await request.get(`${state.base_url}api/v1/auth/session`);
  if (!ui) throw new Error('no answer for the page');
  for (const r of [ui, api]) {
    expect(r.headers()['x-content-type-options']).toBe('nosniff');
    expect(r.headers()['referrer-policy']).toBe('no-referrer');
  }
  expect(api.headers()['content-security-policy']).toBe(
    "default-src 'none'; frame-ancestors 'none'",
  );

  // Every kind of answer of the web port (revision 2.4 candidate 29).
  const asset = await page.evaluate(
    () => document.querySelector<HTMLScriptElement>('script[src]')?.src ?? '',
  );
  expect(asset).not.toBe('');
  for (const url of [
    asset,
    `${state.base_url}api/v1/does-not-exist`,
    `${state.base_url}assets/does-not-exist.js`,
    `${state.base_url}api/v1/users`,
  ]) {
    const r = await request.get(url);
    expect(r.headers()['x-content-type-options'], url).toBe('nosniff');
    expect(r.headers()['referrer-policy'], url).toBe('no-referrer');
    expect(r.headers()['content-security-policy'], url).toContain("frame-ancestors 'none'");
  }
});

// NFR-003: X-Forwarded-For counts only from a proxy in RCD_TRUSTED_PROXIES.
// The stack's proxy adds its client to the header; only the stack basepath
// trusts it. The address shows in the audit entry of the login.
test('X-Forwarded-For counts only from a trusted proxy', async ({ playwright, state, stack }) => {
  const client = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: {
      Origin: new URL(state.base_url).origin,
      'X-Forwarded-For': '203.0.113.9',
    },
  });
  const signIn = await client.post('api/v1/auth/login', {
    data: { pseudonym: 'admin', password: secret(state, 'admin') },
  });
  expect(signIn.status()).toBe(200);
  const audit = await client.get('api/v1/audit?kind=auth');
  const { items } = (await audit.json()) as {
    items: { action: string; payload?: string | { ip?: string } }[];
  };
  const entry = items.find((e) => e.action === 'login');
  const payload =
    typeof entry?.payload === 'string' ? entry.payload : JSON.stringify(entry?.payload);
  expect(payload).toContain(stack === 'basepath' ? '203.0.113.9' : '127.0.0.1');
  expect(payload).not.toContain(stack === 'basepath' ? '127.0.0.1' : '203.0.113.9');
  await client.dispose();
});
