// SPDX-License-Identifier: AGPL-3.0-or-later
import { readFileSync } from 'node:fs';
import { test as base, expect, type Page } from '@playwright/test';

export interface StackState {
  base_url: string;
  users: Record<string, string>;
  reset_user: string;
  reset_password: string;
  server_id: string;
  server_name: string;
}

interface Fixtures {
  stack: string;
  state: StackState;
  csp: string[];
}

// state is what the e2e stack wrote; csp collects policy violations of the
// page, which must stay empty (SEC-009).
export const test = base.extend<Fixtures>({
  stack: ['root', { option: true }],
  state: async ({ stack }, use) => {
    await use(JSON.parse(readFileSync(`e2e/.state-${stack}.json`, 'utf8')) as StackState);
  },
  baseURL: async ({ state }, use) => {
    await use(state.base_url);
  },
  csp: async ({ page }, use) => {
    const violations: string[] = [];
    page.on('console', (m) => {
      if (/Content.Security.Policy/i.test(m.text())) violations.push(m.text());
    });
    await page.addInitScript(() => {
      document.addEventListener('securitypolicyviolation', (e: SecurityPolicyViolationEvent) => {
        console.error(`Content-Security-Policy violation: ${e.violatedDirective} ${e.blockedURI}`);
      });
    });
    await use(violations);
    expect(violations, 'CSP violations').toEqual([]);
  },
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
