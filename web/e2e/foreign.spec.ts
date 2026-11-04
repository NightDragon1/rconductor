// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Locator, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// The server hostile and its container send markup, color codes and an
// override of the reading direction (internal/e2e/stack, SEC-006).
const markup = '<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>';
const override = '‮';

async function open(page: Page, state: StackState, tab: string) {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.hostile}/${tab}`);
}

async function run(page: Page, command: string) {
  await page.getByRole('textbox', { name: 'Command' }).fill(command);
  await page.keyboard.press('Enter');
}

// lefts gives the left edge of every character of the text after the
// control, in the order of the text.
function lefts(box: Locator) {
  return box.evaluate((el) => {
    const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    const out: number[] = [];
    while (walk.nextNode()) {
      const node = walk.currentNode as Text;
      if (!node.data.startsWith('txt.exe')) continue;
      for (let i = 0; i < node.length; i++) {
        const r = document.createRange();
        r.setStart(node, i);
        r.setEnd(node, i + 1);
        out.push(r.getBoundingClientRect().left);
      }
    }
    return out;
  });
}

// harmless checks a box of foreign text: all of it is text, the color codes
// made no style, the override shows and does not act.
async function harmless(page: Page, box: Locator) {
  await expect(box).toContainText(markup);
  await expect(box.locator('img, script')).toHaveCount(0);
  expect(await page.evaluate(() => '__xss' in window)).toBe(false);

  await expect(box).toContainText('hello §zworld');
  await expect(box).not.toContainText('§a');
  await expect(box.locator('[style]')).toHaveCount(0);

  const control = box.locator('[data-code]');
  await expect(control).toHaveCount(1);
  await expect(control).toHaveAttribute('data-code', '⟨U+202E⟩');
  expect(await control.evaluate((el) => getComputedStyle(el, '::before').content)).toContain(
    'U+202E',
  );
  const x = await lefts(box);
  expect(x.length).toBe(7);
  expect(x).toEqual([...x].sort((a, b) => a - b));
}

test('a hostile answer stays text in console and history (SEC-006)', async ({ page, state }) => {
  await open(page, state, 'console');
  await run(page, 'say console');
  const block = page.getByRole('article', { name: 'say console' });
  await harmless(page, block.locator('pre'));

  await page.goto(`servers/${state.servers.hostile}/history`);
  const entry = page
    .getByRole('region', { name: 'History' })
    .getByRole('listitem')
    .filter({ hasText: 'say console' });
  await entry.getByText('Answer', { exact: true }).click();
  await harmless(page, entry.locator('pre'));
});

test('hostile log lines stay text (SEC-006)', async ({ page, state }) => {
  await open(page, state, 'logs');
  await harmless(page, page.getByRole('region', { name: 'Logs' }).locator('pre'));
});

test('copying gives the original text, not the stand-in (SEC-006)', async ({ page, state }) => {
  await open(page, state, 'console');
  await run(page, 'say copy');
  const block = page.getByRole('article', { name: 'say copy' });
  const pre = block.locator('pre');
  await expect(pre).toContainText(markup);

  await page.evaluate(() => {
    navigator.clipboard.writeText = (t) => {
      document.documentElement.dataset.copied = t;
      return Promise.resolve();
    };
  });
  await block.getByRole('button', { name: 'Copy the block' }).click();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.dataset.copied))
    .toContain(`file${override}txt.exe`);

  // Selecting with the mouse gives it too.
  const selected = await pre.evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    return sel?.toString() ?? '';
  });
  expect(selected).toContain(`file${override}txt.exe`);
  expect(selected).not.toContain('U+202E');
});

test('hostile player names and images are plain names (SEC-006)', async ({ page, state }) => {
  await open(page, state, 'overview');
  const players = page.getByRole('main').getByRole('list').filter({ hasText: 'evilgnp.exe' });
  await expect(players).toContainText('<script>window.__xss=1</script>');
  await expect(players).toContainText('evilgnp.exe');
  await expect(players.getByText('Steve', { exact: true })).toBeVisible();
  await expect(players).not.toContainText('§');
  await expect(players).not.toContainText(override);
  await expect(players.locator('script')).toHaveCount(0);

  await page.goto('./');
  const card = page.getByRole('article', { name: 'hostile', exact: true });
  await expect(card).toContainText('<b>evil</b>');
  await expect(card).not.toContainText('§');
  await expect(card.locator('b')).toHaveCount(0);
  expect(await page.evaluate(() => '__xss' in window)).toBe(false);
});

test('a hostile container name is plain text in the picker (SEC-006)', async ({ page, state }) => {
  await login(page, 'admin', secret(state, 'admin'));
  await page.goto('admin/servers');
  await page.getByRole('button', { name: 'hostile' }).first().click();
  const select = page.getByLabel('Container');
  await expect(select.locator('option', { hasText: '<script>' })).toHaveCount(1);
  await expect(select.locator('script')).toHaveCount(0);
  expect(await page.evaluate(() => '__xss' in window)).toBe(false);
});
