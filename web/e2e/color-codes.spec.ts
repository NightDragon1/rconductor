// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Locator, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Color codes (CMD-017): the server hostile
// (profile minecraft-java, formatting minecraft) answers with markup, the
// codes §a and §z and an override of the reading direction
// (internal/e2e/stack). Known codes become styles of the fixed list
// (SEC-006).
// Each test sends its own command: the console of the shared stack keeps
// the blocks of the other tests.

const markup = '<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>';
const fixed = new Set([
  ...'0123456789abcdef'.split('').map((c) => `text-mc-${c}`),
  'font-bold',
  'italic',
  'underline',
  'line-through',
  '[text-decoration-line:underline_line-through]',
]);

async function answer(page: Page, state: StackState, tag: string): Promise<Locator> {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.hostile}/console`);
  await page.getByRole('textbox', { name: 'Command' }).fill(`say colors ${tag}`);
  await page.keyboard.press('Enter');
  const pre = page.getByRole('article', { name: `say colors ${tag}` }).locator('pre');
  await expect(pre).toContainText('hello');
  return pre;
}

// stillText checks what always holds: markup stays text,
// no inline style, the override shows as a stand-in.
async function stillText(page: Page, pre: Locator) {
  await expect(pre).toContainText(markup);
  await expect(pre.locator('img, script')).toHaveCount(0);
  expect(await page.evaluate(() => '__xss' in window)).toBe(false);
  await expect(pre.locator('[style]')).toHaveCount(0);
  await expect(pre.locator('[data-code]')).toHaveAttribute('data-code', '⟨U+202E⟩');
  await expect(pre).toContainText('§zworld');
  await expect(pre).not.toContainText('§a');
}

test.describe('color codes', () => {
  test('a known code becomes a style of the fixed list (CMD-017)', async ({ page, state }) => {
    const pre = await answer(page, state, 'style');
    const green = pre.locator('span.text-mc-a');
    await expect(green).toHaveText('hello §zworld');
    const color = await green.evaluate((el) => getComputedStyle(el).color);
    const plain = await pre.evaluate((el) => getComputedStyle(el).color);
    expect(color).not.toBe(plain);
  });

  test('a hostile answer gets no class or attribute of its own (SEC-006)', async ({
    page,
    state,
  }) => {
    const pre = await answer(page, state, 'hostile');
    await stillText(page, pre);
    const spans = await pre
      .locator('span')
      .evaluateAll((els) =>
        els.map((e) => Array.from(e.getAttributeNames(), (n) => [n, e.getAttribute(n)])),
      );
    expect(spans.length).toBeGreaterThan(0);
    for (const attrs of spans) {
      for (const [name, value] of attrs) {
        expect(name).toBe('class');
        for (const c of (value ?? '').split(' ')) expect(fixed).toContain(c);
      }
    }
  });
});
