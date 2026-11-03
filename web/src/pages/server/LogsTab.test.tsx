// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { addLogLines, resetBuffers, updateBuffer } from '@/lib/console';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { LogsView } from './LogsTab';

const server: Server = {
  id: 's1',
  name: 'mc',
  groups: [],
  status: 'connected',
  status_since: '2026-11-03T10:00:00Z',
  version: 'v',
  agent_id: 'a1',
  runtime: {
    kind: 'runtime',
    server_id: 's1',
    container: {
      name: 'mc',
      state: 'running',
      restart_count: 0,
      restart_loop: false,
      stale: true,
      agent_connected: false,
    },
  },
};

describe('logs tab', () => {
  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    resetBuffers();
    updateBuffer('s1', (b) => addLogLines(b, ['[Server] §6Done§r'], '2026-11-03T10:00:00Z'));
    const { container } = await renderPage(() => <LogsView server={server} />);
    // The log line is foreign data, shown without color codes.
    expect(unmarked(container)).toEqual(['[Server] Done']);
  });
});
