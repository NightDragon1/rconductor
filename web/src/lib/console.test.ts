// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import {
  addCommands,
  addLogLines,
  emptyBuffer,
  MAX_COMMANDS,
  MAX_LOG_LINES,
  ownCommands,
  suggest,
  visible,
  type CmdBlock,
} from './console';

const at = (s: number) => new Date(Date.UTC(2026, 10, 3, 12, 0, s)).toISOString();
const cmd = (id: string, s: number, user = 'me', extra: Partial<CmdBlock> = {}): CmdBlock => ({
  kind: 'cmd',
  id,
  ts: at(s),
  userId: user,
  command: `cmd ${id}`,
  result: 'ok',
  ...extra,
});

describe('console buffer', () => {
  it('merges the answer and the live event of one command (CMD-012)', () => {
    let b = addCommands(emptyBuffer, [cmd('a', 1, 'me', { response: 'done' })]);
    b = addCommands(b, [cmd('a', 1, 'me', { pseudonym: 'admin', role: 'rcon_admin' })]);
    expect(b.cmds).toHaveLength(1);
    expect(b.cmds[0]).toMatchObject({ response: 'done', pseudonym: 'admin' });
  });

  it('keeps commands and log lines apart in their limits (G3)', () => {
    let b = addCommands(
      emptyBuffer,
      Array.from({ length: MAX_COMMANDS + 5 }, (_, i) => cmd(`c${i}`, i)),
    );
    expect(b.cmds).toHaveLength(MAX_COMMANDS);
    expect(b.cmds[0]?.id).toBe('c5');
    for (let i = 0; i < 12; i++) {
      b = addLogLines(
        b,
        Array.from({ length: 50 }, (_, j) => `line ${i}-${j}`),
        at(2000 + i),
      );
    }
    expect(b.logs.reduce((n, l) => n + l.lines.length, 0)).toBe(MAX_LOG_LINES);
    expect(b.logs[0]?.lines[0]).toBe('line 2-0');
    // Logs never push commands out.
    expect(b.cmds).toHaveLength(MAX_COMMANDS);
  });

  it('shows the tracks and the filter (CMD-014)', () => {
    let b = addCommands(emptyBuffer, [
      cmd('a', 1, 'me'),
      cmd('b', 3, 'other', { pseudonym: 'mod' }),
    ]);
    b = addLogLines(b, ['joined'], at(2));
    const all = { own: true, others: true, logs: true };
    expect(visible(b, all, '', 'me').map((x) => x.kind)).toEqual(['cmd', 'log', 'cmd']);
    expect(visible(b, { ...all, others: false }, '', 'me')).toHaveLength(2);
    expect(visible(b, { ...all, logs: false }, '', 'me')).toHaveLength(2);
    expect(visible(b, all, 'MOD', 'me').map((x) => x.id)).toEqual(['b']);
    expect(visible(b, all, 'joined', 'me').map((x) => x.kind)).toEqual(['log']);
  });
});

describe('input (CMD-015)', () => {
  it('suggests command names from the catalog', () => {
    const paths = ['list', 'whitelist add', 'whitelist remove', 'say'];
    expect(suggest(paths, 'whi')).toEqual(['whitelist add', 'whitelist remove']);
    expect(suggest(paths, '')).toEqual([]);
    expect(suggest(paths, 'list')).toEqual([]);
  });

  it('walks the own commands newest first', () => {
    const b = addCommands(emptyBuffer, [
      cmd('a', 1, 'me', { command: 'list' }),
      cmd('b', 2, 'other', { command: 'say x' }),
      cmd('c', 3, 'me', { command: 'say hi' }),
      cmd('d', 4, 'me', { command: 'say hi' }),
    ]);
    expect(ownCommands(b, 'me')).toEqual(['say hi', 'list']);
  });
});
