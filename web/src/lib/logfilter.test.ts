// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import {
  compileFilter,
  MAX_BRACKET_PART,
  MAX_FILTER_LENGTH,
  problemOf,
  splitLine,
  type LogFilter,
} from './logfilter';

const lines = [
  '[10:00:00] [Server thread/INFO]: Steve joined the game',
  "[10:00:01] [Server thread/WARN]: Can't keep up! Is the server overloaded?",
  '[10:00:02] [Worker-Main-1/INFO]: Preparing spawn area: 83%',
  'Starting minecraft server version 26.3',
] as const;

function run(f: Partial<LogFilter>): readonly string[] {
  const c = compileFilter({ bracket: '', text: '', op: 'and', ...f });
  if (!c.ok) throw new Error(c.problem);
  return c.test ? lines.filter(c.test) : lines;
}

describe('splitLine (UI-019)', () => {
  it('takes the leading bracketed groups and the text after the colon', () => {
    expect(splitLine(lines[0])).toEqual({
      bracket: '[10:00:00] [Server thread/INFO]',
      text: 'Steve joined the game',
    });
  });

  it('has no bracket part without a group at the start', () => {
    expect(splitLine(lines[3])).toEqual({ bracket: '', text: lines[3] });
    expect(splitLine('text [INFO] later')).toEqual({ bracket: '', text: 'text [INFO] later' });
    expect(splitLine('')).toEqual({ bracket: '', text: '' });
  });

  it('counts nested brackets and stops at an unclosed group', () => {
    expect(splitLine('[a [b] c] rest')).toEqual({ bracket: '[a [b] c]', text: 'rest' });
    expect(splitLine('[open text')).toEqual({ bracket: '', text: '[open text' });
    expect(splitLine('[x] [open text')).toEqual({ bracket: '[x]', text: '[open text' });
    expect(splitLine('[[[[')).toEqual({ bracket: '', text: '[[[[' });
    expect(splitLine(']] text')).toEqual({ bracket: '', text: ']] text' });
  });

  it('keeps control characters in the bracket part as data', () => {
    const s = splitLine('[Server\u0000\u001b[31m/INFO‮]] tail');
    expect(s.bracket).toContain('\u0000');
    expect(s.text).toBe('tail');
  });

  it('splits a 1 MiB line without looking far into it', () => {
    const big = '[A/INFO]: ' + 'x'.repeat(1 << 20);
    const s = splitLine(big);
    expect(s.bracket).toBe('[A/INFO]');
    expect(s.text).toHaveLength(1 << 20);
    // An unclosed bracket run over the whole line gives no bracket part.
    const open = '['.repeat(1 << 20);
    expect(splitLine(open).bracket).toBe('');
    // Groups beyond the scan limit stay in the text.
    const many = '[a] '.repeat(MAX_BRACKET_PART);
    expect(splitLine(many).bracket.length).toBeLessThanOrEqual(MAX_BRACKET_PART);
  });
});

describe('compileFilter (UI-019)', () => {
  it('filters nothing with both parts empty', () => {
    expect(run({})).toEqual(lines);
    expect(run({ bracket: '  ', text: ' ', op: 'xor' })).toEqual(lines);
  });

  it('matches each part as a case-insensitive substring', () => {
    expect(run({ bracket: 'warn' })).toEqual([lines[1]]);
    expect(run({ text: 'STEVE' })).toEqual([lines[0]]);
    // The text filter does not see the bracket part.
    expect(run({ text: 'thread' })).toEqual([]);
    // A line without bracket part never matches a bracket filter.
    expect(run({ bracket: 'minecraft' })).toEqual([]);
    expect(run({ text: 'minecraft' })).toEqual([lines[3]]);
  });

  it('combines with and, or, xor', () => {
    const f = { bracket: 'server thread', text: 'steve' };
    expect(run({ ...f, op: 'and' })).toEqual([lines[0]]);
    expect(run({ ...f, op: 'or' })).toEqual([lines[0], lines[1]]);
    expect(run({ ...f, op: 'xor' })).toEqual([lines[1]]);
  });

  it('lets one non-empty part decide whatever the operator', () => {
    for (const op of ['and', 'or', 'xor'] as const) {
      expect(run({ bracket: 'info', op })).toEqual([lines[0], lines[2]]);
    }
  });

  it('treats regular-expression syntax as plain text', () => {
    expect(run({ text: '(a+)+$' })).toEqual([]);
    expect(run({ text: 'up!' })).toEqual([lines[1]]);
    expect(run({ bracket: '[10:00:02]' })).toEqual([lines[2]]);
    expect(run({ text: '83%' })).toEqual([lines[2]]);
  });

  it('refuses control characters and overlong parts with a problem', () => {
    expect(problemOf('ok')).toBeUndefined();
    expect(compileFilter({ bracket: 'a\u0007', text: '', op: 'and' })).toEqual({
      ok: false,
      problem: 'control',
    });
    expect(
      compileFilter({ bracket: '', text: 'x'.repeat(MAX_FILTER_LENGTH + 1), op: 'or' }),
    ).toEqual({ ok: false, problem: 'tooLong' });
    expect(compileFilter({ bracket: '', text: 'x'.repeat(MAX_FILTER_LENGTH), op: 'or' }).ok).toBe(
      true,
    );
  });

  it('falls back to and for an unknown operator', () => {
    const c = compileFilter({ bracket: 'server', text: 'steve', op: 'nand' as never });
    expect(c.ok && c.test ? lines.filter(c.test) : []).toEqual([lines[0]]);
  });

  it('matches a 1 MiB line in either part', () => {
    const big = '[A/INFO]: ' + 'y'.repeat(1 << 20) + 'needle';
    const c = compileFilter({ bracket: 'info', text: 'needle', op: 'and' });
    expect(c.ok && c.test?.(big)).toBe(true);
  });
});
