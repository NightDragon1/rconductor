// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { aboutKey, type About } from '@/api/about';
import { sessionKey } from '@/api/session';
import { Shell } from '@/pages/Shell';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { AboutContent } from './AboutDialog';

const anonymous: About = {
  name: 'RCONductor',
  version: '1.2.0',
  license: 'AGPL-3.0-or-later',
  repo_url: 'https://github.com/NightDragon1/rconductor',
};

const admin: About = {
  ...anonymous,
  commit: '0123456789abcdef',
  build_date: '2026-11-15T10:00:00Z',
  agents: [
    { id: 'a1', name: 'same‮major', version: 'v1.0.3', connected: true },
    { id: 'a2', name: 'older', version: '0.9.1', connected: false },
    { id: 'a3', name: 'odd', version: 'unknown', connected: true },
  ],
};

function renderAbout(about: About) {
  return renderPage(() => <AboutContent about={about} />);
}

describe('About dialog (UI-024)', () => {
  it('shows name, version, author and licenses to an anonymous answer', async () => {
    await setLanguage('en');
    await renderAbout(anonymous);
    expect(screen.getByText('RCONductor')).toBeTruthy();
    expect(screen.getByText('1.2.0')).toBeTruthy();
    expect(screen.getByText('NightDragon')).toBeTruthy();
    expect(screen.getByText('ShadowGhost.NET')).toBeTruthy();
    expect(
      screen.getByText(
        'Licensed under AGPL-3.0-or-later. This program comes with ABSOLUTELY NO WARRANTY.',
      ),
    ).toBeTruthy();
    const repo = screen.getByRole('link', { name: 'https://github.com/NightDragon1/rconductor' });
    expect(repo.getAttribute('href')).toBe(anonymous.repo_url);
    expect(repo.closest('p')?.textContent).toBe(
      'For more see https://github.com/NightDragon1/rconductor',
    );
    const license = screen.getByRole('link', { name: 'License text' });
    expect(license.getAttribute('href')).toBe(
      'https://github.com/NightDragon1/rconductor/blob/main/LICENSE',
    );
    const third = screen.getByRole('link', { name: 'Third-party licenses' });
    expect(third.getAttribute('href')).toBe('third-party-licenses.txt');
    for (const a of screen.getAllByRole('link')) {
      expect(a.getAttribute('target')).toBe('_blank');
      expect(a.getAttribute('rel')).toBe('noopener noreferrer');
    }
    // Build and agents only when the panel sends them.
    expect(screen.queryByText('Commit')).toBeNull();
    expect(screen.queryByText('Build date')).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Agents' })).toBeNull();
  });

  it('shows build and agents to the RCON admin, warns for another major', async () => {
    await setLanguage('en');
    const { container } = await renderAbout(admin);
    expect(screen.getByText('0123456789abcdef').tagName).toBe('CODE');
    expect(screen.getByText('2026-11-15T10:00:00Z').getAttribute('datetime')).toBe(
      '2026-11-15T10:00:00Z',
    );
    const list = within(screen.getByRole('region', { name: 'Agents' })).getAllByRole('listitem');
    expect(list).toHaveLength(3);
    const [same, older, odd] = list as [HTMLElement, HTMLElement, HTMLElement];
    expect(within(same).queryByRole('status')).toBeNull();
    expect(within(older).getByRole('status').textContent).toBe(
      'Its major version differs from the panel (1). Update the agent or the panel.',
    );
    expect(within(older).getByText('not connected')).toBeTruthy();
    // unknown is shown as it is, without a warning.
    expect(within(odd).getByText('unknown')).toBeTruthy();
    expect(within(odd).queryByRole('status')).toBeNull();
    // Name and version are foreign data: isolated, bidi controls visible.
    expect(same.querySelector('[data-code]')?.getAttribute('data-code')).toBe('⟨U+202E⟩');
    expect(within(older).getByText('older').tagName).toBe('BDI');
    expect(within(older).getByText('0.9.1').tagName).toBe('BDI');
    expect(container.querySelector('img[src]')).toBeTruthy();
  });

  it('shows markup in an agent name as text', async () => {
    await setLanguage('en');
    const { container } = await renderAbout({
      ...admin,
      agents: [{ id: 'x', name: '<img src=x onerror=alert(1)>', version: '9.0', connected: true }],
    });
    expect(container.querySelector('img[src="x"]')).toBeNull();
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeTruthy();
  });

  it('says so when no agent is paired', async () => {
    await setLanguage('en');
    await renderAbout({ ...admin, agents: [] });
    expect(screen.getByText('No agents are paired.')).toBeTruthy();
  });

  it('makes no link of an address other than http(s)', async () => {
    await setLanguage('en');
    await renderAbout({ ...anonymous, repo_url: 'javascript:alert(1)' });
    expect(screen.queryByRole('link', { name: 'javascript:alert(1)' })).toBeNull();
    expect(screen.getByText('javascript:alert(1)')).toBeTruthy();
  });

  it('has no text outside the language files besides the data (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderAbout(admin);
    const data = [...container.querySelectorAll('bdi, code, time')].map((e) => e.textContent ?? '');
    expect(unmarked(container).filter((s) => !data.some((d) => d.includes(s)))).toEqual([]);
  });

  it('opens from the entry at the foot of the navigation', async () => {
    await setLanguage('en');
    const session = {
      user: { id: 'u1', pseudonym: 'admin', role: 'rcon_admin' },
      expires_at: '2026-11-03T00:00:00Z',
      must_change_password: false,
      ui: { shortcuts_enabled: false, supporters_enabled: false, supporters_allowed: false },
      panel_version: '1.2.0',
    };
    await renderPage(Shell, [
      [sessionKey, session],
      [aboutKey('u1'), admin],
    ]);
    const nav = screen.getByRole('navigation', { name: 'Main navigation' });
    // The entry is no link of the navigation lists.
    expect(within(nav).queryByText('About')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'About' }));
    const dialog = await screen.findByRole('dialog', { name: 'About RCONductor' });
    expect(within(dialog).getByText('0123456789abcdef')).toBeTruthy();
    expect(within(dialog).getAllByRole('listitem')).toHaveLength(3);
  });
});
