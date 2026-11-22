// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { aboutKey } from '@/api/about';
import { api } from '@/api/client';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { LoginPage } from './Login';

const about = {
  name: 'RCONductor',
  version: '1.2.0',
  license: 'AGPL-3.0-or-later',
  repo_url: 'https://github.com/NightDragon1/rconductor',
};

afterEach(() => {
  vi.restoreAllMocks();
});

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
    // An answer that is still on its way (#214).
    const get = vi.spyOn(api, 'GET').mockReturnValue(new Promise(() => {}) as never);
    await renderPage(LoginPage);
    expect(get).toHaveBeenCalled();
    expect(screen.queryByRole('link', { name: 'GitHub' })).toBeNull();
    expect(screen.queryByText(/AGPL/)).toBeNull();
  });

  it('makes no link of a repository address other than http(s) (#213)', async () => {
    await setLanguage('en');
    for (const repo_url of ['javascript:alert(1)', '//evil.example', 'ftp://x.example']) {
      const { unmount } = await renderPage(LoginPage, [[aboutKey(), { ...about, repo_url }]]);
      expect(screen.queryByRole('link', { name: 'GitHub' })).toBeNull();
      expect(screen.getByText(/RCONductor 1.2.0/).textContent).toContain('GitHub');
      unmount();
    }
  });

  it('drops the answers of an earlier session at sign-in (#216)', async () => {
    await setLanguage('en');
    vi.spyOn(api, 'POST').mockResolvedValue({
      data: { must_change_password: false, totp_setup_required: false },
      response: new Response(null, { status: 200 }),
    } as never);
    const { qc } = await renderPage(LoginPage, [
      [aboutKey(), about],
      [aboutKey('u1'), { ...about, commit: 'old' }],
    ]);
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'u' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'p' } });
    fireEvent.submit(screen.getByLabelText('Password').closest('form') as HTMLFormElement);
    await waitFor(() => expect(qc.getQueryData(aboutKey('u1'))).toBeUndefined());
  });

  it('takes the line from the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(LoginPage, [[aboutKey(), about]]);
    expect(unmarked(container)).toEqual([]);
  });
});
