// SPDX-License-Identifier: AGPL-3.0-or-later
import { readFileSync } from 'node:fs';
import { expect, login, secret, test } from './fixtures';

// The token of an invitation travels only in the fragment of the link
// and in the body of the redemption: never in a request path or query,
// never in the panel's log (revision 2.4 candidate 23).
test('an invitation link keeps its token out of paths and logs (UI-008)', async ({
  page,
  browser,
  state,
  stack,
}) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  const created = await page.evaluate(async () => {
    const r = await fetch('api/v1/invites', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role: 'supporter' }),
    });
    return (await r.json()) as { token: string; url: string };
  });
  expect(new URL(created.url).hash).toBe(`#${created.token}`);
  expect(new URL(created.url).pathname).toMatch(/\/invite$/);

  const guest = await browser.newContext();
  const invitee = await guest.newPage();
  const urls: string[] = [];
  const bodies: string[] = [];
  invitee.on('request', (r) => {
    urls.push(r.url());
    if (r.method() === 'POST') bodies.push(r.postData() ?? '');
  });
  await invitee.goto(created.url);
  await expect(invitee.getByRole('heading', { name: 'Accept the invitation' })).toBeVisible();
  // The address bar loses the token at once.
  expect(new URL(invitee.url()).hash).toBe('');
  await invitee.getByLabel('Pseudonym').fill(`invited-${Date.now() % 100000}`);
  await invitee.getByLabel('Password', { exact: true }).fill('a fine invited password');
  await invitee.getByLabel('Repeat the new password').fill('a fine invited password');
  await invitee.getByRole('button', { name: 'Create my account' }).click();
  await expect(invitee.getByRole('status')).toContainText('Your account is ready');
  await guest.close();

  for (const u of urls) {
    const { pathname, search } = new URL(u);
    expect(pathname + search, u).not.toContain(created.token);
  }
  expect(bodies.some((b) => b.includes(created.token))).toBe(true);
  expect(readFileSync(`e2e/.panel-${stack}.log`, 'utf8')).not.toContain(created.token);
});

test('an expired invitation says so (UI-008)', async ({ page, state }) => {
  await page.goto(`invite#${state.expired_invite}`);
  await page.getByLabel('Pseudonym').fill('too-late');
  await page.getByLabel('Password', { exact: true }).fill('a fine invited password');
  await page.getByLabel('Repeat the new password').fill('a fine invited password');
  await page.getByRole('button', { name: 'Create my account' }).click();
  await expect(page.getByRole('alert')).toHaveText(
    'This invitation is not valid or has expired. Ask for a new link.',
  );
});
