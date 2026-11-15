// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { formatLines, MAX_STYLED } from './formatting';

const fixed = new Set([
  ...'0123456789abcdef'.split('').map((c) => `text-mc-${c}`),
  'font-bold',
  'italic',
  'underline',
  'line-through',
  '[text-decoration-line:underline_line-through]',
]);
const classes = (text: string) =>
  formatLines(text)
    .flat()
    .flatMap((s) => s.className.split(' ').filter(Boolean));

describe('formatLines (CMD-017)', () => {
  it('turns color and format codes into styles of the fixed list', () => {
    expect(formatLines('§aGreen §lbold§r plain')).toEqual([
      [
        { text: 'Green ', className: 'text-mc-a' },
        { text: 'bold', className: 'text-mc-a font-bold' },
        { text: ' plain', className: '' },
      ],
    ]);
    expect(formatLines('§n§mboth')).toEqual([
      [{ text: 'both', className: '[text-decoration-line:underline_line-through]' }],
    ]);
    expect(formatLines('§oit§Ax')).toEqual([
      [
        { text: 'it', className: 'italic' },
        { text: 'x', className: 'text-mc-a' },
      ],
    ]);
  });

  it('lets a color end the formats, as the game does', () => {
    expect(formatLines('§l§obold§cred')).toEqual([
      [
        { text: 'bold', className: 'font-bold italic' },
        { text: 'red', className: 'text-mc-c' },
      ],
    ]);
  });

  it('keeps unknown and cut codes as text, like with the flag off', () => {
    expect(formatLines('hello §zworld')).toEqual([[{ text: 'hello §zworld', className: '' }]]);
    expect(formatLines('end §')).toEqual([[{ text: 'end §', className: '' }]]);
    // The Kelvin sign lowercases to k but is no code, like with the flag off.
    expect(formatLines('§\u212Ax')).toEqual([[{ text: '§\u212Ax', className: '' }]]);
    expect(formatLines('§§aa')).toEqual([
      [
        { text: '§', className: '' },
        { text: 'a', className: 'text-mc-a' },
      ],
    ]);
  });

  it('drops free hex colors and obfuscation, they are not in the list', () => {
    expect(formatLines('§x§F§F§0§0§0§0red§kx')).toEqual([[{ text: 'redx', className: '' }]]);
    // A cut sequence drops only §x; the rest are plain color codes.
    expect(formatLines('§x§F§Fcut')).toEqual([[{ text: 'cut', className: 'text-mc-f' }]]);
  });

  it('ends styles at a line break', () => {
    expect(formatLines('§aone\ntwo')).toEqual([
      [{ text: 'one', className: 'text-mc-a' }],
      [{ text: 'two', className: '' }],
    ]);
  });

  it('never takes a class from the answer', () => {
    const hostile = '§a<b class="x" style="color:red">§"onerror§lx§ §‮§r§1§9';
    for (const c of classes(hostile)) expect(fixed).toContain(c);
    expect(
      formatLines(hostile)
        .flat()
        .map((s) => s.text)
        .join(''),
    ).toContain('<b class="x" style="color:red">');
  });

  it('caps the styled segments of a large answer, the rest without codes (1 MiB)', () => {
    const big = '§ax§by'.repeat(Math.ceil((1 << 20) / 6));
    expect(big.length).toBeGreaterThanOrEqual(1 << 20);
    const segs = formatLines(big).flat();
    expect(segs.filter((s) => s.className).length).toBe(MAX_STYLED);
    const rest = segs[segs.length - 1];
    expect(rest?.className).toBe('');
    expect(rest?.text).not.toContain('§');
    const mib = 'x'.repeat(1 << 20);
    expect(formatLines(`§a${mib}`)).toEqual([[{ text: mib, className: 'text-mc-a' }]]);
  });

  it('caps across lines too', () => {
    const lines = Array.from({ length: MAX_STYLED + 5 }, () => '§ax').join('\n');
    const out = formatLines(lines);
    expect(out.flat().filter((s) => s.className).length).toBe(MAX_STYLED);
    expect(out[out.length - 1]).toEqual([{ text: 'x', className: '' }]);
  });
});
