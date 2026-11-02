// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// Issue #88 (NFR-003): reloading a deep link loads every asset, also below
// the base path; SEC-009: the headers are on every answer and the page runs
// without a policy violation.
test('a deep link loads after a reload', async ({ page, state, csp }) => {
  void csp;
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
});
