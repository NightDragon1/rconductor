// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { sessionKey } from '@/api/session';
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

const session = (features: string[]) => ({
  user: { id: 'boss', pseudonym: 'boss', role: 'rcon_admin' },
  ui: { features, shortcuts_enabled: false, supporters_enabled: false, supporters_allowed: false },
  panel_version: '0.10',
});

const logLines = [
  '[10:00:00] [Server thread/INFO]: Steve joined the game',
  '[10:00:01] [Server thread/WARN]: Steve is lagging',
  '[10:00:02] [Worker/INFO]: tick',
];

async function renderLogs(features: string[]) {
  await setLanguage('en');
  resetBuffers();
  updateBuffer('s1', (b) => addLogLines(b, logLines, '2026-11-03T10:00:00Z'));
  return renderPage(() => <LogsView server={server} />, [[sessionKey, session(features)]]);
}

describe('two-part log filter (UI-019)', () => {
  it('keeps the single filter while the flag is off', async () => {
    await renderLogs([]);
    expect(screen.getByRole('searchbox', { name: 'Filter the logs' })).toBeTruthy();
    expect(screen.queryByRole('searchbox', { name: 'Filter the bracket part' })).toBeNull();
    expect(screen.queryByRole('combobox')).toBeNull();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the logs' }), {
      target: { value: 'thread' },
    });
    const text = screen.getByLabelText('Log lines').textContent ?? '';
    expect(text).toContain('Steve joined');
    expect(text).not.toContain('tick');
  });

  it('filters bracket part and text with and, or, xor while the flag is on', async () => {
    await renderLogs(['log_filter_split']);
    expect(screen.queryByRole('searchbox', { name: 'Filter the logs' })).toBeNull();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the bracket part' }), {
      target: { value: 'server thread' },
    });
    fireEvent.change(
      screen.getByRole('searchbox', { name: 'Filter the text after the brackets' }),
      {
        target: { value: 'joined' },
      },
    );
    const shown = () => screen.getByLabelText('Log lines').textContent ?? '';
    expect(shown()).toContain('Steve joined');
    expect(shown()).not.toContain('lagging');
    const op = screen.getByRole('combobox', { name: 'Combine the two filters' });
    fireEvent.change(op, { target: { value: 'xor' } });
    expect(shown()).toContain('lagging');
    expect(shown()).not.toContain('joined');
    fireEvent.change(op, { target: { value: 'or' } });
    expect(shown()).toContain('lagging');
    expect(shown()).toContain('joined');
    expect(shown()).not.toContain('tick');
  });

  it('shows a hint for a refused filter and keeps all lines', async () => {
    await renderLogs(['log_filter_split']);
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the bracket part' }), {
      target: { value: 'a\u0007b' },
    });
    expect(screen.getByText(/contains control characters/)).toBeTruthy();
    expect(screen.getByLabelText('Log lines').textContent).toContain('tick');
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the bracket part' }), {
      target: { value: 'x'.repeat(10_000) },
    });
    expect(screen.getByText(/more than 200 characters/)).toBeTruthy();
  });

  it('shows a 1 MiB line with controls in its bracket part as foreign text', async () => {
    await setLanguage('en');
    resetBuffers();
    const big = '[Srv‮/INFO]: ' + 'z'.repeat(1 << 20);
    updateBuffer('s1', (b) => addLogLines(b, [big], '2026-11-03T10:00:00Z'));
    const { container } = await renderPage(
      () => <LogsView server={server} />,
      [[sessionKey, session(['log_filter_split'])]],
    );
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter the bracket part' }), {
      target: { value: 'info' },
    });
    // The bidi control is shown as a stand-in by ForeignText.
    expect(container.querySelector('pre bdi [data-code]')).not.toBeNull();
    expect(screen.getByLabelText('Log lines').textContent?.length).toBeGreaterThan(1 << 20);
  });
});
