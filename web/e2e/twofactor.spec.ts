// SPDX-License-Identifier: AGPL-3.0-or-later
import { createHmac } from 'node:crypto';
import type { APIRequestContext, Page } from '@playwright/test';
import { expect, login, secret, test } from './fixtures';

// code is the TOTP code of RFC 6238 (SHA-1, 6 digits, 30 s) for a base32
// secret at the step now + offset.
function code(base32: string, offset = 0): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const c of base32.replace(/=+$/, ''))
    bits += alphabet.indexOf(c).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g)?.map((b) => parseInt(b, 2)) ?? []);
  const step = Math.floor(Date.now() / 30_000) + offset;
  const msg = Buffer.alloc(8);
  msg.writeBigUInt64BE(BigInt(step));
  const h = createHmac('sha1', key).update(msg).digest();
  const o = h[h.length - 1] & 0x0f;
  const v = (h.readUInt32BE(o) & 0x7fffffff) % 1_000_000;
  return String(v).padStart(6, '0');
}

const password = 'the password of the 2fa user';

// freshUser invites a supporter-less moderator of its own, so that the
// other tests keep their users.
async function freshUser(
  admin: APIRequestContext,
  anon: APIRequestContext,
  origin: string,
): Promise<string> {
  const name = `tfa-${Date.now()}`;
  const created = await admin.post('api/v1/invites', {
    data: { role: 'moderator' },
    headers: { Origin: origin },
  });
  expect(created.status()).toBe(201);
  const inv = await created.json();
  const r = await anon.post('api/v1/invites/redeem', {
    data: { token: inv.token, pseudonym: name, password },
    headers: { Origin: origin },
  });
  expect(r.status()).toBe(201);
  return name;
}

async function signedIn(page: Page) {
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
}

test('two-factor sign-in: set up, sign in with the code, reset by the admin (AUTH-008)', async ({
  page,
  browser,
  state,
}) => {
  const origin = new URL(state.base_url).origin;
  const adminCtx = await browser.newContext({ baseURL: state.base_url });
  const adminPage = await adminCtx.newPage();
  await login(adminPage, 'admin', secret(state, 'admin'));
  await signedIn(adminPage);
  const anon = await browser.newContext({ baseURL: state.base_url });
  const name = await freshUser(adminPage.request, anon.request, origin);
  await anon.close();

  await login(page, name, password);
  await signedIn(page);
  await page.goto('profile');
  const section = page.getByRole('region', { name: 'Two-factor sign-in' });
  await expect(section).toContainText('Off');
  await section.getByLabel('Your password').fill(password);
  await section.getByRole('button', { name: 'Set up' }).click();
  await expect(
    section.getByRole('img', { name: 'QR code for the authenticator app' }),
  ).toBeVisible();
  const key = (await section.locator('code').first().textContent()) ?? '';
  await section.getByLabel('Code from the app').fill(code(key));
  await section.getByRole('button', { name: 'Switch on' }).click();
  await expect(section).toContainText('Two-factor sign-in is on.');

  // Signing in again needs the code.
  await page.context().clearCookies();
  await login(page, name, password);
  await expect(page.getByRole('heading', { name: 'Enter the code' })).toBeVisible();
  await page.getByLabel('Code from the app').fill('000000');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toHaveText('The code is wrong.');
  await page.getByLabel('Code from the app').fill(code(key, 1));
  await page.getByRole('button', { name: 'Sign in' }).click();
  await signedIn(page);

  // The admin resets it; the session ends, the next login is one step.
  const users = await (await adminPage.request.get('api/v1/users')).json();
  const id = users.items.find((u: { pseudonym: string }) => u.pseudonym === name).id;
  await adminPage.goto('admin/users');
  await adminPage.getByRole('button', { name, exact: true }).click();
  const panel = adminPage.getByRole('complementary', { name: `Details of ${name}` });
  await expect(panel).toContainText('Two-factor');
  await panel.getByRole('button', { name: 'Reset second factor' }).click();
  await adminPage.getByRole('dialog').getByRole('button', { name: 'Reset second factor' }).click();
  await expect(panel.getByRole('status')).toHaveText('Second factor reset.');
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  await login(page, name, password);
  await signedIn(page);
  expect(id).toBeTruthy();
  await adminCtx.close();
});

test('a required second factor allows only its setup (AUTH-008)', async ({
  page,
  browser,
  state,
}) => {
  const origin = new URL(state.base_url).origin;
  const adminCtx = await browser.newContext({ baseURL: state.base_url });
  const adminPage = await adminCtx.newPage();
  await login(adminPage, 'admin', secret(state, 'admin'));
  await signedIn(adminPage);
  const req = adminPage.request;
  const anon = await browser.newContext({ baseURL: state.base_url });
  const name = await freshUser(req, anon.request, origin);
  await anon.close();
  const headers = { Origin: origin };
  // The requirement holds for the admin too: he sets up his own first, so
  // that he can switch it off again.
  const setup = await (
    await req.post('api/v1/me/second-factor/setup', {
      data: { password: secret(state, 'admin') },
      headers,
    })
  ).json();
  expect(
    (
      await req.post('api/v1/me/second-factor/confirm', {
        data: { code: code(setup.secret) },
        headers,
      })
    ).status(),
  ).toBe(204);
  const setRequired = async (on: boolean) => {
    const r0 = await req.get('api/v1/settings');
    const r = await req.patch('api/v1/settings', {
      data: { 'auth.totp_required': on },
      headers: { ...headers, 'If-Match': r0.headers().etag ?? '' },
    });
    expect(r.status()).toBe(200);
  };
  await setRequired(true);
  try {
    await login(page, name, password);
    await expect(page.getByRole('heading', { name: 'Set up two-factor sign-in' })).toBeVisible();
    await page.goto('servers');
    await expect(page.getByRole('heading', { name: 'Set up two-factor sign-in' })).toBeVisible();
    const r = await page.request.get('api/v1/servers');
    expect(r.status()).toBe(403);
    expect((await r.json()).reason).toBe('totp_setup_required');
  } finally {
    await setRequired(false);
    expect(
      (
        await req.post('api/v1/me/second-factor/disable', {
          data: { password: secret(state, 'admin'), code: code(setup.secret, 1) },
          headers,
        })
      ).status(),
    ).toBe(204);
    await adminCtx.close();
  }
});
