// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { ServerActions } from './ServerActions';

const server: Server = {
  id: 's1',
  name: 'mc',
  groups: [],
  status: 'connected',
  status_since: '2026-11-03T10:00:00Z',
  agent_id: 'a1',
  runtime: {
    kind: 'runtime',
    server_id: 's1',
    container: {
      name: 'mc',
      state: 'running',
      restart_count: 0,
      restart_loop: false,
      stale: false,
      agent_connected: true,
    },
  },
};

describe('server actions', () => {
  it('have no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    await renderPage(() => <ServerActions server={server} role="server_admin" />);
    const stop = screen.getAllByRole('button').at(-1);
    if (stop) fireEvent.click(stop);
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(unmarked(document.body)).toEqual([]);
  });

  it('warn about the restart mechanism without agent (W6)', async () => {
    setLanguage('en');
    const plain: Server = { ...server, agent_id: undefined, runtime: undefined };
    await renderPage(() => <ServerActions server={plain} role="rcon_admin" />);
    expect(screen.getAllByRole('button').map((b) => b.textContent)).toEqual(['Restart over RCON']);
    fireEvent.click(screen.getByRole('button', { name: 'Restart over RCON' }));
    expect(screen.getByRole('dialog').textContent).toContain(
      'on-failure does not apply to a clean shutdown',
    );
  });

  it('are absent for moderators (AGT-008)', async () => {
    setLanguage('en');
    await renderPage(() => <ServerActions server={server} role="moderator" />);
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });
});
