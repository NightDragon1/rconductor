// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { aboutKey } from '@/api/about';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { LoginPage } from './Login';

const about = {
  name: 'RCONductor',
  version: '1.2.0',
  license: 'AGPL-3.0-or-later',
  repo_url: 'https://github.com/NightDragon1/rconductor',
};

describe('login page (UI-024)', () => {
  it('shows one line with version, license and the repository', async () => {
    await setLanguage('en');
    await renderPage(LoginPage, [[aboutKey(), about]]);
    const link = screen.getByRole('link', { name: 'GitHub' });
    expect(link.getAttribute('href')).toBe(about.repo_url);
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toBe('noopener noreferrer');
    expect(link.closest('p')?.textContent).toBe('RCONductor 1.2.0 · AGPL-3.0-or-later · GitHub');
  });

  it('shows no line before the answer', async () => {
    await setLanguage('en');
    await renderPage(LoginPage, [[aboutKey(), undefined]]);
    expect(screen.queryByRole('link', { name: 'GitHub' })).toBeNull();
    expect(screen.queryByText(/AGPL/)).toBeNull();
  });

  it('takes the line from the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(LoginPage, [[aboutKey(), about]]);
    expect(unmarked(container)).toEqual([]);
  });
});
