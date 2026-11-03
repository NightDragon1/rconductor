// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ChangePasswordPage } from '@/pages/ChangePassword';
import { LoginPage } from '@/pages/Login';
import { meKey } from '@/api/me';
import { sessionKey } from '@/api/session';
import { NotFoundPage } from '@/pages/NotFound';
import { Shell } from '@/pages/Shell';
import { renderPage, unmarked, setLanguage } from '@/test/render';

// NFR-007: every visible text and label comes from the language files; a
// test language changes all of them.
describe('language files', () => {
  for (const [name, Page] of Object.entries({ LoginPage, ChangePasswordPage, NotFoundPage })) {
    it(`${name} has no text outside the language files`, async () => {
      setLanguage('xx');
      const { container } = await renderPage(Page);
      expect(unmarked(container)).toEqual([]);
    });
  }

  it('Shell has no text outside the language files', async () => {
    setLanguage('xx');
    const session = {
      user: { id: '1', pseudonym: 'admin', role: 'rcon_admin' },
      expires_at: '2026-11-03T00:00:00Z',
      must_change_password: false,
      ui: { shortcuts_enabled: false, supporters_enabled: false },
    };
    const { container } = await renderPage(Shell, [
      [sessionKey, session],
      [meKey, { supporters_allowed: false }],
    ]);
    // The pseudonym and its initials are data, not texts.
    expect(unmarked(container).filter((s) => s !== 'admin' && s !== 'AD')).toEqual([]);
  });

  it('shows English by default', async () => {
    setLanguage('en');
    await renderPage(LoginPage);
    expect(screen.getByRole('heading').textContent).toBe('Sign in');
  });
});
