// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// A URL outside the role shows page not found and sends no request but the
// session's: the interface only hides, the backend enforces every right
// (CLAUDE.md, Design 6.2).
const urls: Record<string, string> = {
  audit: 'Audit',
  'admin/users': 'Users',
  'admin/groups': 'Groups',
  'admin/command-lists': 'Command lists',
  'admin/servers': 'Servers and groups',
  'admin/agents': 'Agents',
  'admin/settings': 'Settings',
};
const allowed: Record<string, string[]> = {
  admin: Object.keys(urls),
  sadmin: ['audit', 'admin/users', 'admin/groups', 'admin/command-lists', 'admin/servers'],
  mod: ['admin/users'],
  mod2: [],
  sup: [],
};

for (const [user, ok] of Object.entries(allowed)) {
  test(`direct URLs of ${user}`, async ({ page, state }) => {
    await login(page, user, secret(state, user));
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
    for (const [url, title] of Object.entries(urls)) {
      const requests: string[] = [];
      const record = (r: { url(): string }) => {
        const u = new URL(r.url());
        if (u.pathname.includes('/api/v1/') && !u.pathname.endsWith('/api/v1/auth/session')) {
          requests.push(u.pathname);
        }
      };
      page.on('request', record);
      await page.goto(url);
      if (ok.includes(url)) {
        await expect(page.getByRole('heading', { name: title })).toBeVisible();
      } else {
        await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
        await page.waitForLoadState('networkidle');
        expect(requests, `${user} ${url}`).toEqual([]);
      }
      page.off('request', record);
    }
  });
}
