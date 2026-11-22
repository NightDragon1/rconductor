// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { aboutKey, type About } from '@/api/about';
import { renderPage, setLanguage } from '@/test/render';
import { AboutButton, AboutContent } from './AboutDialog';

const cached: About = {
  name: 'RCONductor',
  version: '1.2.0',
  license: 'AGPL-3.0-or-later',
  repo_url: 'https://github.com/NightDragon1/rconductor',
  commit: 'cafecafecafecafe',
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe('About dialog gaps (UI-024, D-080)', () => {
  it('without a known user neither shows the empty entry nor fetches', async () => {
    await setLanguage('en');
    const get = vi.spyOn(api, 'GET');
    await renderPage(() => <AboutButton collapsed={false} />, [[aboutKey(''), cached]]);
    fireEvent.click(screen.getByRole('button', { name: 'About' }));
    const dialog = await screen.findByRole('dialog', { name: 'About RCONductor' });
    expect(within(dialog).queryByText('cafecafecafecafe')).toBeNull();
    expect(get).not.toHaveBeenCalledWith('/about');
  });

  it('shows odd versions and dates as they are, without a warning', async () => {
    await setLanguage('en');
    const long = '9'.repeat(64);
    await renderPage(() => (
      <AboutContent
        about={{
          ...cached,
          version: 'dev',
          build_date: 'yesterday',
          agents: [{ id: 'a1', name: 'a', version: long, connected: true }],
        }}
      />
    ));
    expect(screen.getByText('dev')).toBeTruthy();
    // a build date that is no date is text, not a time element
    expect(screen.getByText('yesterday').tagName).toBe('BDI');
    fireEvent.click(screen.getByRole('tab', { name: 'Agents' }));
    const item = within(screen.getByRole('region', { name: 'Agents' })).getByRole('listitem');
    expect(within(item).getByText(long)).toBeTruthy();
    // the panel version has no number: no major to compare, no warning
    expect(within(item).queryByRole('status')).toBeNull();
  });
});
