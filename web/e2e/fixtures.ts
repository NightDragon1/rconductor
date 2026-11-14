// SPDX-License-Identifier: AGPL-3.0-or-later
import { readFileSync } from 'node:fs';
import { test as base, expect, type APIRequestContext, type Page } from '@playwright/test';

export interface StackState {
  base_url: string;
  users: Record<string, string>;
  reset_user: string;
  reset_password: string;
  server_id: string;
  server_name: string;
  servers: Record<string, string>;
  legacy_agent_id: string;
  locked_user_id: string;
  expired_invite: string;
  control_url: string;
}

interface Fixtures {
  stack: string;
  state: StackState;
  csp: string[];
  flags: string[];
  flagSwitch: undefined;
}

// state is what the e2e stack wrote; csp runs with every test and collects
// the policy violations of the page, which must stay empty (SEC-009).
export const test = base.extend<Fixtures>({
  stack: ['root', { option: true }],
  state: async ({ stack }, use) => {
    await use(JSON.parse(readFileSync(`e2e/.state-${stack}.json`, 'utf8')) as StackState);
  },
  baseURL: async ({ state }, use) => {
    await use(state.base_url);
  },
  // flags names the feature flags a test needs (NFR-019). The RCON admin
  // switches them on through the settings API before the test and off
  // again afterwards, also when the test fails: no back door in the panel.
  flags: [[], { option: true }],
  flagSwitch: [
    async ({ flags, playwright, state }, use) => {
      if (flags.length === 0) {
        await use(undefined);
        return;
      }
      const admin = await playwright.request.newContext({
        baseURL: state.base_url,
        extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
      });
      try {
        const r = await admin.post('api/v1/auth/login', {
          data: { pseudonym: 'admin', password: secret(state, 'admin') },
        });
        expect(r.status(), 'admin login for the flag switch').toBe(200);
        await setFlags(admin, flags, true);
        try {
          await use(undefined);
        } finally {
          await setFlags(admin, flags, false);
        }
      } finally {
        await admin.dispose();
      }
    },
    { auto: true },
  ],
  csp: [
    async ({ page }, use) => {
      const violations: string[] = [];
      page.on('console', (m) => {
        if (/Content.Security.Policy/i.test(m.text())) violations.push(m.text());
      });
      await page.addInitScript(() => {
        document.addEventListener('securitypolicyviolation', (e: SecurityPolicyViolationEvent) => {
          console.error(
            `Content-Security-Policy violation: ${e.violatedDirective} ${e.blockedURI}`,
          );
        });
      });
      await use(violations);
      expect(violations, 'CSP violations').toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };

// secret returns the password of a test user from the stack state.
export function secret(state: StackState, user: string): string {
  const p = state.users[user];
  if (!p) throw new Error(`no test user ${user}`);
  return p;
}

export async function login(page: Page, pseudonym: string, password: string) {
  await page.goto('login');
  await page.getByLabel('Pseudonym').fill(pseudonym);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

// setFlags writes features.<name> for every flag in one versioned change
// (If-Match, revision 2.4 candidate 24) and checks that the panel took it.
export async function setFlags(admin: APIRequestContext, flags: string[], on: boolean) {
  const etag = (await admin.get('api/v1/settings')).headers()['etag'] ?? '';
  const data = Object.fromEntries(flags.map((f) => [`features.${f}`, on]));
  const r = await admin.patch('api/v1/settings', { data, headers: { 'If-Match': etag } });
  expect(r.status(), `switch ${flags.join(', ')} ${on ? 'on' : 'off'}`).toBe(200);
  const now = (await r.json()) as Record<string, unknown>;
  for (const f of flags) expect(now[`features.${f}`], `features.${f}`).toBe(on);
}
