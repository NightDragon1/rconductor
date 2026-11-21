// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { APIRequest, Page } from '@playwright/test';
import { expect, login, secret, test, type StackState } from './fixtures';

// Messages as conversations (MSG-001 to MSG-003): the list on the left, the
// message with its comments as a thread on the right, a reply adds a
// comment; "New message" opens the form in a dialog. Below 768 px the list
// and the message take turns (Design 6.1).

async function enter(page: Page, state: StackState, user: string) {
  await login(page, user, secret(state, user));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto('messages');
  await expect(page.getByRole('heading', { name: 'Messages', level: 1 })).toBeVisible();
}

// cleanup deletes the messages of a test as the RCON admin, also when the
// test fails, so that no unread message is left for other specs.
async function cleanup(playwright: { request: APIRequest }, state: StackState, prefix: string) {
  const admin = await playwright.request.newContext({
    baseURL: state.base_url,
    extraHTTPHeaders: { Origin: new URL(state.base_url).origin },
  });
  try {
    await admin.post('api/v1/auth/login', {
      data: { pseudonym: 'admin', password: secret(state, 'admin') },
    });
    const list = (await (await admin.get('api/v1/messages?limit=100')).json()) as {
      items: { id: string; text: string }[];
    };
    for (const m of list.items.filter((x) => x.text.startsWith(prefix))) {
      await admin.delete(`api/v1/messages/${m.id}`);
    }
  } finally {
    await admin.dispose();
  }
}

test('the RCON admin writes, the moderator reads and replies in the thread', async ({
  browser,
  page,
  playwright,
  state,
}) => {
  const subject = `e2e conversation ${Date.now()}`;
  try {
    await enter(page, state, 'admin');
    await page.getByRole('button', { name: 'New message' }).click();
    const dialog = page.getByRole('dialog', { name: 'New message' });
    await dialog.getByLabel('To').selectOption('users');
    await dialog.getByRole('checkbox', { name: /^mod\s/ }).check();
    await dialog.getByLabel('Text').fill(`${subject}\nThe second line.`);
    await dialog.getByRole('button', { name: 'Send' }).click();
    await expect(dialog.getByRole('status')).toHaveText('Sent to 1 recipient.');
    await dialog.getByRole('button', { name: 'Close' }).click();

    const other = await browser.newContext();
    try {
      const mod = await other.newPage();
      await enter(mod, state, 'mod');
      const item = mod.getByTestId('message').filter({ hasText: subject });
      // Unread: bold, with its mark; the title is the first line only.
      await expect(item.getByRole('button')).toHaveClass(/font-semibold/);
      await expect(item).toContainText('new');
      await expect(item).not.toContainText('The second line.');
      await item.getByRole('button').click();
      const view = mod.getByRole('article', { name: 'Message' });
      await expect(view).toContainText('The second line.');
      // A recipient gets no delete and no recipient count.
      await expect(view.getByRole('button', { name: 'Delete message' })).toHaveCount(0);
      await expect(view).not.toContainText('recipient');
      await view.getByLabel('Your reply').fill('Got it.');
      await view.getByRole('button', { name: 'Reply' }).click();
      await expect(view.getByTestId('comment')).toContainText('Got it.');
      await expect(item.getByRole('button')).not.toHaveClass(/font-semibold/);
    } finally {
      await other.close();
    }

    await page.reload();
    const mine = page.getByTestId('message').filter({ hasText: subject });
    await mine.getByRole('button').click();
    const view = page.getByRole('article', { name: 'Message' });
    await expect(view).toContainText('1 recipient');
    await expect(view.getByTestId('comment')).toContainText('Got it.');
    await view.getByRole('button', { name: 'Delete message' }).click();
    await page
      .getByRole('dialog', { name: 'Delete this message?' })
      .getByRole('button', { name: 'Delete message' })
      .click();
    await expect(mine).toHaveCount(0);
    await expect(view).toHaveCount(0);
  } finally {
    await cleanup(playwright, state, subject);
  }
});

test('the server admin writes to his servers, a supporter writes nothing', async ({
  page,
  state,
}) => {
  await enter(page, state, 'sadmin');
  await page.getByRole('button', { name: 'New message' }).click();
  const to = page.getByRole('dialog', { name: 'New message' }).getByLabel('To');
  await expect(to.locator('option')).toHaveText([
    'Everyone on my servers',
    'Moderators on my servers',
  ]);
  await page.keyboard.press('Escape');

  await page.context().clearCookies();
  await enter(page, state, 'sup');
  await expect(page.getByRole('button', { name: 'New message' })).toHaveCount(0);
});

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the list, then the message with back and reply', async ({ page, playwright, state }) => {
    const subject = `e2e phone ${Date.now()}`;
    try {
      await enter(page, state, 'admin');
      await page.getByRole('button', { name: 'New message' }).click();
      const dialog = page.getByRole('dialog', { name: 'New message' });
      await dialog.getByLabel('To').selectOption('users');
      await dialog.getByRole('checkbox', { name: /^mod2\s/ }).check();
      await dialog.getByLabel('Text').fill(subject);
      await dialog.getByRole('button', { name: 'Send' }).click();
      await expect(dialog.getByRole('status')).toBeVisible();
      await dialog.getByRole('button', { name: 'Close' }).click();

      const list = page.getByRole('list', { name: 'Messages' });
      await list.getByTestId('message').filter({ hasText: subject }).getByRole('button').click();
      const view = page.getByRole('article', { name: 'Message' });
      // One pane at a time: the list gives way to the message.
      await expect(view).toBeVisible();
      await expect(list).toBeHidden();
      await view.getByLabel('Your reply').fill('From the phone.');
      await view.getByRole('button', { name: 'Reply' }).click();
      await expect(view.getByTestId('comment')).toContainText('From the phone.');
      const width = await page.evaluate(() => document.documentElement.scrollWidth);
      expect(width).toBeLessThanOrEqual(390);
      await view.getByRole('button', { name: 'Back to messages' }).click();
      await expect(list).toBeVisible();
      await expect(view).toHaveCount(0);
    } finally {
      await cleanup(playwright, state, subject);
    }
  });
});
