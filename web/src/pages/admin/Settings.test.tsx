// SPDX-License-Identifier: AGPL-3.0-or-later
import { cleanup, fireEvent, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { renderPage, setLanguage } from '@/test/render';
import { SettingsPage, settingsKey } from './Settings';

const data: [readonly unknown[], unknown][] = [
  [settingsKey, { values: { 'ui.shortcuts_enabled': false }, etag: '"1"' }],
];
const help = 'Single keys for frequent actions, for everyone.';

describe('explanations in the settings (UI-014)', () => {
  beforeEach(async () => {
    localStorage.clear();
    await setLanguage('en');
  });

  it('hides the explanations and keeps that after a reload', async () => {
    await renderPage(SettingsPage, data);
    expect(screen.queryByText(help)).not.toBeNull();
    const toggle = screen.getByRole('checkbox', { name: 'Show explanations' });
    expect((toggle as HTMLInputElement).checked).toBe(true);

    fireEvent.click(toggle);
    expect(screen.queryByText(help)).toBeNull();

    cleanup();
    await renderPage(SettingsPage, data);
    expect(
      (screen.getByRole('checkbox', { name: 'Show explanations' }) as HTMLInputElement).checked,
    ).toBe(false);
    expect(screen.queryByText(help)).toBeNull();
    expect(screen.queryByText('Keyboard shortcuts')).not.toBeNull();

    fireEvent.click(screen.getByRole('checkbox', { name: 'Show explanations' }));
    expect(screen.queryByText(help)).not.toBeNull();
  });

  it('shows the explanations when the browser storage is unavailable', async () => {
    const original = Storage.prototype.getItem;
    Storage.prototype.getItem = () => {
      throw new Error('blocked');
    };
    try {
      await renderPage(SettingsPage, data);
      expect(screen.queryByText(help)).not.toBeNull();
    } finally {
      Storage.prototype.getItem = original;
    }
  });
});
