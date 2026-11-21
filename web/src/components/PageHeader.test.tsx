// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PageHeader } from './PageHeader';

// Every page heading carries one grey line of description (O4).
describe('PageHeader', () => {
  it('shows the heading, its description in grey and further lines', () => {
    render(
      <PageHeader title="Agents" description="One agent per host." actions={<button>Pair</button>}>
        <p>2 paired</p>
      </PageHeader>,
    );
    expect(screen.getByRole('heading', { level: 1, name: 'Agents' })).toBeTruthy();
    expect(screen.getByText('One agent per host.').className).toContain('text-muted');
    expect(screen.getByText('2 paired')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Pair' })).toBeTruthy();
  });

  it('stands alone without actions', () => {
    const { container } = render(<PageHeader title="Audit" description="Who did what." />);
    expect(container.firstElementChild?.firstElementChild?.tagName).toBe('H1');
  });
});
