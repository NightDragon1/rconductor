// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ChangePasswordPage } from '@/pages/ChangePassword';
import { LoginPage } from '@/pages/Login';
import { NotFoundPage } from '@/pages/NotFound';
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

  it('shows English by default', async () => {
    setLanguage('en');
    await renderPage(LoginPage);
    expect(screen.getByRole('heading').textContent).toBe('Sign in');
  });
});
