// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { aboutKey, parseThirdParty, type About } from '@/api/about';
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

const mit = 'MIT License\n\nCopyright (c) 2023 shadcn\n\nPermission is hereby granted';
const apache = [
  'Apache License',
  'Version 2.0, January 2004',
  '   2. Grant of Copyright License. Subject to the terms',
  '      copyright notice that is included in or attached to the work',
  '   APPENDIX: How to apply the Apache License to your work.',
  '      Copyright [yyyy] [name of copyright owner]',
].join('\n');
const goList = `== github.com/coder/websocket v1.8.14 (ISC)\nCopyright (c) 2025 Coder\n\n== Go standard library go1.27.4 (BSD-3-Clause)\nCopyright 2009 The Go Authors.\n`;
const uiList = `== react 19.2.0 (MIT)\n${mit}\n\n== some-lib 2.0.0 (Apache-2.0)\n${apache}\n`;

// lists answers the two license lists; a missing one answers 404.
function lists(files: Record<string, string>) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(((url: string) => {
    const body = files[url];
    return Promise.resolve(
      body === undefined ? new Response('', { status: 404 }) : new Response(body),
    );
  }) as never);
}

beforeEach(() => {
  lists({});
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('About dialog (UI-024)', () => {
  it('shows name, version, author and licenses to an anonymous answer', async () => {
    await setLanguage('en');
    await renderAbout(anonymous);
    expect(screen.getByText('RCONductor')).toBeTruthy();
    expect(screen.getByText('1.2.0')).toBeTruthy();
    expect(screen.getByText('© 2026 NightDragon')).toBeTruthy();
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
    await screen.findByRole('tab', { name: 'Third-party (0)' });
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
      'About',
      'Third-party (0)',
    ]);
    expect(screen.queryByRole('tab', { name: 'Agents' })).toBeNull();
  });

  it('shows build and agents to the RCON admin, warns for another major', async () => {
    await setLanguage('en');
    const { container } = await renderAbout(admin);
    expect(screen.getByText('0123456789abcdef').tagName).toBe('CODE');
    expect(screen.getByText('2026-11-15T10:00:00Z').getAttribute('datetime')).toBe(
      '2026-11-15T10:00:00Z',
    );
    fireEvent.click(screen.getByRole('tab', { name: 'Agents' }));
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
    fireEvent.click(screen.getByRole('tab', { name: 'About' }));
    expect(container.querySelector('img[src]')).toBeTruthy();
  });

  it('lists the Go modules and the UI packages with license and copyright', async () => {
    await setLanguage('en');
    lists({ 'third-party-go.txt': goList, 'third-party-licenses.txt': uiList });
    await renderAbout(anonymous);
    const tab = await screen.findByRole('tab', { name: 'Third-party (4)' });
    fireEvent.click(tab);
    expect(tab.getAttribute('aria-selected')).toBe('true');
    const items = within(screen.getByRole('tabpanel')).getAllByRole('listitem');
    expect(items.map((i) => i.querySelector('bdi')?.textContent)).toEqual([
      'github.com/coder/websocket v1.8.14',
      'Go standard library go1.27.4',
      'react 19.2.0',
      'some-lib 2.0.0',
    ]);
    const [ws, , react, lib] = items as [HTMLElement, HTMLElement, HTMLElement, HTMLElement];
    expect(within(ws).getByText('Copyright (c) 2025 Coder')).toBeTruthy();
    expect(within(react).getByText('Copyright (c) 2023 shadcn')).toBeTruthy();
    expect(within(react).queryByText('see license text')).toBeNull();
    // No copyright line: the license text opens below.
    expect(within(lib).getByText('see license text').tagName).toBe('SUMMARY');
    expect(lib.querySelector('pre')?.textContent).toContain('Apache License');
  });

  it('shows the UI list alone when the Go list is missing', async () => {
    await setLanguage('en');
    lists({ 'third-party-licenses.txt': uiList });
    await renderAbout(anonymous);
    fireEvent.click(await screen.findByRole('tab', { name: 'Third-party (2)' }));
    expect(within(screen.getByRole('tabpanel')).getAllByRole('listitem')).toHaveLength(2);
  });

  it('says so when the build has no list', async () => {
    await setLanguage('en');
    await renderAbout(anonymous);
    fireEvent.click(await screen.findByRole('tab', { name: 'Third-party (0)' }));
    expect(screen.getByText('This build has no list of third-party components.')).toBeTruthy();
  });

  it('moves between the tabs with the arrow keys', async () => {
    await setLanguage('en');
    await renderAbout(admin);
    const about = screen.getByRole('tab', { name: 'About' });
    fireEvent.keyDown(about, { key: 'ArrowLeft' });
    const agents = screen.getByRole('tab', { name: 'Agents' });
    expect(agents.getAttribute('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(agents);
    fireEvent.keyDown(agents, { key: 'ArrowRight' });
    expect(about.getAttribute('aria-selected')).toBe('true');
  });

  it('shows markup in an agent name as text', async () => {
    await setLanguage('en');
    const { container } = await renderAbout({
      ...admin,
      agents: [{ id: 'x', name: '<img src=x onerror=alert(1)>', version: '9.0', connected: true }],
    });
    fireEvent.click(screen.getByRole('tab', { name: 'Agents' }));
    expect(container.querySelector('img[src="x"]')).toBeNull();
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeTruthy();
  });

  it('says so when no agent is paired', async () => {
    await setLanguage('en');
    await renderAbout({ ...admin, agents: [] });
    fireEvent.click(screen.getByRole('tab', { name: 'Agents' }));
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
    fireEvent.click(within(dialog).getByRole('tab', { name: 'Agents' }));
    expect(within(dialog).getAllByRole('listitem')).toHaveLength(3);
  });
});

describe('license lists (UI-024, NFR-021)', () => {
  it('reads header, license and copyright', () => {
    expect(parseThirdParty(goList)).toEqual([
      {
        name: 'github.com/coder/websocket',
        version: 'v1.8.14',
        license: 'ISC',
        copyright: ['Copyright (c) 2025 Coder'],
        text: 'Copyright (c) 2025 Coder',
      },
      {
        name: 'Go standard library',
        version: 'go1.27.4',
        license: 'BSD-3-Clause',
        copyright: ['Copyright 2009 The Go Authors.'],
        text: 'Copyright 2009 The Go Authors.',
      },
    ]);
  });

  it('keeps a header without license text and a compound license', () => {
    expect(parseThirdParty('== a 1.0.0 ()\n== b 2.0.0 ((MIT OR Apache-2.0))\n© 2020 B')).toEqual([
      { name: 'a', version: '1.0.0', license: '', copyright: [], text: '' },
      {
        name: 'b',
        version: '2.0.0',
        license: '(MIT OR Apache-2.0)',
        copyright: ['© 2020 B'],
        text: '© 2020 B',
      },
    ]);
  });

  it('finds no copyright in the Apache text, not the placeholder of its appendix', () => {
    const [c] = parseThirdParty(`== x 1.0 (Apache-2.0)\n${apache}`);
    expect(c?.copyright).toEqual([]);
    expect(c?.text).toContain('Copyright [yyyy]');
  });

  it('ignores text before the first header and reads no list from a page', () => {
    expect(parseThirdParty('<!doctype html><html></html>')).toEqual([]);
    expect(parseThirdParty('')).toEqual([]);
    expect(parseThirdParty('stray\n== a 1 (MIT)\nCopyright A')).toHaveLength(1);
  });

  it('reads a large list', () => {
    const one = `== pkg 1.0.0 (MIT)\n${mit}\n${'line of license text\n'.repeat(200)}`;
    const big = Array.from({ length: 2000 }, () => one).join('');
    const out = parseThirdParty(big);
    expect(out).toHaveLength(2000);
    expect(out[1999]?.copyright).toEqual(['Copyright (c) 2023 shadcn']);
  });
});
