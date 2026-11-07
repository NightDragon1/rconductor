// SPDX-License-Identifier: AGPL-3.0-or-later
import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import {
  addCommands,
  addDelivery,
  addLogLines,
  isRunning,
  RUNNING_MS,
  GAP_LINE,
  bufferOwner,
  bufferWriter,
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
  type ConsoleBuffer,
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

describe('bufferWriter (Design 6.3)', () => {
  const buffers = (server: string) => renderHook(() => useBuffer(server)).result.current;

  it('writes for the user who asked, not for the next one', () => {
    resetBuffers();
    bufferOwner('alice');
    const late = bufferWriter();
    late('s', (b) => ({ ...b, running: 1 }));
    expect(buffers('s').running).toBe(1);

    bufferOwner('bob');
    late('s', (b) => ({ ...b, running: 2 }));
    expect(buffers('s').running).toBeUndefined();

    // A later sign-in of the same user does not bring old answers back.
    bufferOwner('alice');
    late('s', (b) => ({ ...b, running: 3 }));
    expect(buffers('s').running).toBeUndefined();
    bufferOwner(undefined);
  });
});

describe('a command without an answer (#150)', () => {
  const block = (ts: string): CmdBlock => ({
    kind: 'cmd',
    id: '1',
    ts,
    command: 'list',
    result: 'error',
    reason: 'not_answered',
  });

  it('counts as running for a while, then as failed', () => {
    const at = Date.parse('2026-11-06T10:00:00Z');
    const b = block('2026-11-06T10:00:00Z');
    expect(isRunning(b, at + RUNNING_MS - 1)).toBe(true);
    expect(isRunning(b, at + RUNNING_MS)).toBe(false);
    expect(isRunning({ ...b, reason: 'timeout' }, at)).toBe(false);
  });
});

describe('log lines by number', () => {
  const ts = '2026-11-06T10:00:00Z';
  const lines = (b: ConsoleBuffer) => b.logs.flatMap((l) => l.lines);
  const held = (n: number) =>
    addDelivery(
      emptyBuffer,
      { lines: Array.from({ length: n }, () => 'tick'), first: 1, run: 'r' },
      ts,
    );

  it('keeps repeated lines after coming back (#157)', () => {
    const again = Array.from({ length: 50 }, () => 'tick');
    const b = addDelivery(held(50), { lines: again, first: 11, run: 'r', current: true }, ts);
    expect(lines(b)).toHaveLength(60);
    expect(b.logPos).toEqual({ run: 'r', last: 60 });
  });

  it('takes nothing twice from a repeated subscription (#149)', () => {
    const b = held(3);
    const same = addDelivery(b, { lines: ['tick'], first: 3, run: 'r', current: true }, ts);
    expect(lines(same)).toHaveLength(3);
  });

  it('marks a gap (K-6)', () => {
    const b = addDelivery(held(3), { lines: ['n'], first: 9, run: 'r' }, ts);
    expect(lines(b).slice(-2)).toEqual([GAP_LINE, 'n']);
  });

  it('starts over with a new count, by text for a new subscription', () => {
    const b = addDelivery(
      held(2),
      { lines: ['tick', 'new'], first: 1, run: 's', current: true },
      ts,
    );
    expect(lines(b)).toEqual(['tick', 'tick', 'new']);
    expect(b.logPos).toEqual({ run: 's', last: 2 });
  });

  it('does not repeat lines after Clear (K-7)', () => {
    const cleared = { ...held(3), logs: [] };
    const b = addDelivery(cleared, { lines: ['tick'], first: 3, run: 'r', current: true }, ts);
    expect(lines(b)).toEqual([]);
  });
});

describe('earlier lines', () => {
  const ts = '2026-11-06T10:00:00Z';
  const lines = (b: ConsoleBuffer) => b.logs.flatMap((l) => l.lines);

  it('do not double a buffer the tail goes past (#154)', () => {
    const b = addLogLines(emptyBuffer, ['h1', 'h2', 'h3'], ts);
    const after = prependTail(b, ['e1', 'h1', 'h2', 'h3', 'n1']);
    expect(lines(after)).toEqual(['e1', 'h1', 'h2', 'h3']);
  });

  it('stay out of a full buffer (K-7)', () => {
    const full = Array.from({ length: MAX_LOG_LINES }, (_, i) => `l${i}`);
    const b = addLogLines(emptyBuffer, full, ts);
    expect(prependTail(b, ['x', 'y'])).toBe(b);
  });
});
