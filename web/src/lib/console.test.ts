// SPDX-License-Identifier: AGPL-3.0-or-later
import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import {
  addCommands,
  addLogLines,
  bufferOwner,
  newLines,
  prependTail,
  emptyBuffer,
  MAX_COMMANDS,
  MAX_LOG_LINES,
  ownCommands,
  resetBuffers,
  suggest,
  updateBuffer,
  useBuffer,
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

describe('earlier log lines (revision 2.4 candidate 20)', () => {
  it('puts only the lines before the overlap in front', () => {
    const b = addLogLines(emptyBuffer, ['c', 'd', 'e'], at(10));
    const after = prependTail(b, ['a', 'b', 'c', 'd']);
    expect(after.logs.flatMap((l) => l.lines)).toEqual(['a', 'b', 'c', 'd', 'e']);
    expect(Date.parse(after.logs[0]?.ts ?? '')).toBeLessThan(Date.parse(at(10)));
    // Nothing new: the buffer stays.
    expect(prependTail(after, ['a', 'b'])).toBe(after);
    // No overlap: all lines go in front.
    expect(prependTail(b, ['x', 'y']).logs.flatMap((l) => l.lines)).toEqual([
      'x',
      'y',
      'c',
      'd',
      'e',
    ]);
  });

  it('keeps the limit of log lines', () => {
    const b = addLogLines(
      emptyBuffer,
      Array.from({ length: MAX_LOG_LINES - 2 }, (_, i) => `n${i}`),
      at(5),
    );
    const after = prependTail(b, ['o1', 'o2', 'o3', 'o4']);
    const lines = after.logs.flatMap((l) => l.lines);
    expect(lines).toHaveLength(MAX_LOG_LINES);
    expect(lines.slice(0, 3)).toEqual(['o3', 'o4', 'n0']);
  });
});

describe('owner of the buffers (Design 6.3)', () => {
  const held = () => renderHook(() => useBuffer('s1')).result.current.logs.length;

  it('forgets everything when another user signs in', () => {
    bufferOwner('admin');
    updateBuffer('s1', (b) => addLogLines(b, ['op secret'], '2026-11-06T10:00:00Z'));
    expect(held()).toBe(1);
    bufferOwner('moderator');
    expect(held()).toBe(0);
  });

  it('forgets everything on signing out, also what arrives afterwards', () => {
    bufferOwner('admin');
    updateBuffer('s1', (b) => addLogLines(b, ['first'], '2026-11-06T10:00:00Z'));
    bufferOwner(undefined);
    expect(held()).toBe(0);
    updateBuffer('s1', (b) => addLogLines(b, ['late answer'], '2026-11-06T10:00:01Z'));
    bufferOwner('admin');
    expect(held()).toBe(0);
  });

  it('keeps the buffers of the same user', () => {
    bufferOwner('admin');
    updateBuffer('s1', (b) => addLogLines(b, ['mine'], '2026-11-06T10:00:00Z'));
    bufferOwner('admin');
    expect(held()).toBe(1);
    resetBuffers();
  });
});

describe('repeated log lines (#149)', () => {
  const buf = (lines: string[]) => addLogLines(emptyBuffer, lines, '2026-11-06T10:00:00Z');

  it('adds nothing when all lines are here', () => {
    expect(newLines(buf(['a', 'b', 'c']), ['b', 'c'])).toEqual([]);
    expect(newLines(buf(['a', 'b', 'c']), ['a', 'b', 'c'])).toEqual([]);
  });

  it('adds only what followed', () => {
    expect(newLines(buf(['a', 'b']), ['a', 'b', 'c', 'd'])).toEqual(['c', 'd']);
  });

  it('keeps lines that repeat in the log itself', () => {
    expect(newLines(buf(['x', 'x']), ['x', 'x', 'x'])).toEqual(['x']);
  });

  it('adds all lines after a gap or into an empty buffer', () => {
    expect(newLines(buf(['a']), ['y', 'z'])).toEqual(['y', 'z']);
    expect(newLines(emptyBuffer, ['y'])).toEqual(['y']);
  });
});
