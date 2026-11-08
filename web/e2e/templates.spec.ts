// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, login, secret, test } from './fixtures';

// CMD-008 (D-068): the admin keeps templates in the console; the
// moderator sees only templates of his list and runs them through the
// pipeline like a typed command.
test('templates: the admin keeps them, the moderator runs those of his list (CMD-008)', async ({
  page,
  state,
}) => {
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/console`);
  await page.getByRole('button', { name: 'Templates' }).click();
  const dialog = page.getByRole('dialog', { name: 'Templates' });
  for (const [name, text] of [
    ['Kick for spam', 'kick {player} spam'],
    ['Kick all', 'kickall {player}'],
  ]) {
    await dialog.getByRole('button', { name: 'New template' }).click();
    const editor = dialog.getByRole('form', { name: 'New template' });
    await editor.getByLabel('Name').fill(name);
    await editor.getByLabel('Command').fill(text);
    await editor.getByRole('button', { name: 'Save' }).click();
    await expect(dialog.getByRole('form', { name, exact: true })).toBeVisible();
  }
  // The same name again is refused with the backend's reason (UI-016).
  await dialog.getByRole('button', { name: 'New template' }).click();
  const again = dialog.getByRole('form', { name: 'New template' });
  await again.getByLabel('Name').fill('kick for spam');
  await again.getByLabel('Command').fill('kick {player}');
  await again.getByRole('button', { name: 'Save' }).click();
  await expect(again.getByRole('alert')).toContainText('template with this name');
  await again.getByRole('button', { name: 'Cancel' }).click();
  // Filled in, the command goes to the input line.
  const kick = dialog.getByRole('form', { name: 'Kick for spam', exact: true });
  await kick.getByLabel('player', { exact: true }).fill('Steve');
  await kick.getByRole('button', { name: 'Put into the input' }).click();
  await expect(page.getByRole('textbox', { name: 'Command' })).toHaveValue('kick Steve spam');

  // The moderator has kick on his list, not kickall.
  await page.context().clearCookies();
  await login(page, 'mod', secret(state, 'mod'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}`);
  const templates = page.getByRole('region', { name: 'Templates' });
  await expect(templates.getByRole('form')).toHaveCount(1);
  const card = templates.getByRole('form', { name: 'Kick for spam', exact: true });
  await expect(card.getByRole('button', { name: 'Run' })).toBeDisabled();
  await card.getByLabel('player', { exact: true }).fill('Steve');
  await card.getByRole('button', { name: 'Run' }).click();
  await expect(page.getByRole('region', { name: 'My history' })).toContainText('kick Steve spam');

  // Cleaned up for the other specs.
  await page.context().clearCookies();
  await login(page, 'admin', secret(state, 'admin'));
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
  await page.goto(`servers/${state.servers.survival}/console`);
  await page.getByRole('button', { name: 'Templates' }).click();
  for (const name of ['Kick for spam', 'Kick all']) {
    await dialog.getByRole('button', { name: `Delete ${name}` }).click();
    await page
      .getByRole('dialog', { name: `Delete the template ${name}?` })
      .getByRole('button', { name: 'Delete' })
      .click();
    await expect(dialog.getByRole('form', { name, exact: true })).toHaveCount(0);
  }
});
