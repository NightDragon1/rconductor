// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Card, Cards } from './Cards';

describe('cards', () => {
  it('open the details from the title', () => {
    const onOpen = vi.fn();
    render(
      <Cards label="Users">
        <Card title="mod" onOpen={onOpen} facts={[{ label: 'Role', value: 'Moderator' }]} />
      </Cards>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'mod' }));
    expect(onOpen).toHaveBeenCalledOnce();
    expect(screen.getByRole('list', { name: 'Users' }).textContent).toContain('RoleModerator');
  });
  it('have no button without details and show the empty text', () => {
    render(
      <Cards label="Users" empty="None">
        <Card title="(invitation)" facts={[]} />
      </Cards>,
    );
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByText('None')).toBeTruthy();
  });
});
