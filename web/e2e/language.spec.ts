// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';
import { expect, secret, test, type StackState } from './fixtures';
import { pages, serverTabs } from './pages';

// NFR-007: this stack serves a build whose language file has every text
// marked. A visible text or label without the mark does not come from a
// language file, unless it is data: foreign data and names stand in <bdi>,
// and a label may repeat them.
const dataTexts = [
  /^(admin|sadmin|mod|sup|[A-Z]{2})$/, // pseudonym and its initials
  /^Minecraft \(Java Edition\)$/, // name in the game profile
  /^SHA256:/, // key fingerprint
];

async function unmarked(page: Page, names: string[]) {
  const found = await page.evaluate(() => {
    const texts: string[] = [];
    const data = new Set<string>();
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const s = n.textContent?.trim() ?? '';
      if (!s || s.includes('⟦') || !/\p{L}/u.test(s)) continue;
      if (n.parentElement?.closest('bdi, time, code, pre, script, style')) data.add(s);
      else texts.push(s);
    }
    for (const e of Array.from(document.querySelectorAll('[aria-label],[title],[placeholder]'))) {
      for (const a of ['aria-label', 'title', 'placeholder']) {
        const s = e.getAttribute(a);
        if (s && !s.includes('⟦') && !data.has(s)) texts.push(s);
      }
    }
    return texts;
  });
  return found.filter((s) => !names.includes(s) && !dataTexts.some((d) => d.test(s)));
}

async function check(page: Page, state: StackState, what: string) {
  await page.waitForLoadState('networkidle').catch(() => undefined);
  expect
    .soft(await unmarked(page, [...Object.keys(state.servers), state.reset_user]), what)
    .toEqual([]);
}

async function signIn(page: Page, user: string, password: string) {
  await page.goto('login');
  await page.getByLabel('⟦Pseudonym⟧').fill(user);
  await page.getByLabel('⟦Password⟧', { exact: true }).fill(password);
  await page.getByRole('button', { name: '⟦Sign in⟧' }).click();
  await expect(page.getByRole('navigation').first()).toBeVisible();
}

test('the check finds a text outside the language files', async ({ page }) => {
  await page.goto('login');
  await expect(page.getByLabel('⟦Pseudonym⟧')).toBeVisible();
  await page.evaluate(() => {
    document.body.insertAdjacentHTML(
      'beforeend',
      '<p>Plain text</p><b aria-label="Plain label"></b>',
    );
  });
  expect(await unmarked(page, [])).toEqual(['Plain text', 'Plain label']);
});

test('the sign-in page and its error', async ({ page, state }) => {
  await page.goto('login');
  await check(page, state, 'login');
  await page.getByLabel('⟦Pseudonym⟧').fill('nobody');
  await page.getByLabel('⟦Password⟧', { exact: true }).fill('wrong');
  await page.getByRole('button', { name: '⟦Sign in⟧' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await check(page, state, 'failed sign-in');
});

for (const [user, paths] of Object.entries(pages)) {
  test(`the pages of ${user}`, async ({ page, state }) => {
    await signIn(page, user, secret(state, user));
    for (const path of paths) {
      await page.goto(path || './');
      await check(page, state, `${user} /${path}`);
    }
  });
}

test('the pages of a server', async ({ page, state }) => {
  await signIn(page, 'admin', secret(state, 'admin'));
  for (const tab of serverTabs) {
    await page.goto(`servers/${state.server_id}${tab}`);
    await check(page, state, `server ${tab}`);
  }
});

test('a page that does not exist', async ({ page, state }) => {
  await signIn(page, 'admin', secret(state, 'admin'));
  await page.goto('no-such-page');
  await check(page, state, 'not found');
});

test('the dialogs and menus', async ({ page, state }) => {
  await signIn(page, 'admin', secret(state, 'admin'));
  await page.goto(`servers/${state.server_id}`);
  await page
    .getByRole('group', { name: '⟦Server actions⟧' })
    .getByRole('button', { name: '⟦Stop⟧' })
    .click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await check(page, state, 'confirm dialog');
  await page.keyboard.press('Escape');

  await page.goto('admin/users');
  await page
    .getByRole('button', { name: /⟦Invite|invitation/i })
    .first()
    .click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await check(page, state, 'invite dialog');
  await page.keyboard.press('Escape');

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`servers/${state.server_id}`);
  await page.getByRole('button', { name: '⟦Server actions⟧' }).click();
  await expect(page.getByRole('menu')).toBeVisible();
  await check(page, state, 'actions menu');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '⟦Menu⟧' }).click();
  await expect(page.getByRole('dialog', { name: '⟦Menu⟧' })).toBeVisible();
  await check(page, state, 'phone menu');
});
