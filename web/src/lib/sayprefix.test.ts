// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { byteCompare, prefixProblem, winningGroup } from './sayprefix';

describe('say prefix (CMD-020, D-082)', () => {
  it('orders names by UTF-8 bytes like the backend', () => {
    expect(byteCompare('Zeta', 'alpha')).toBeLessThan(0);
    expect(byteCompare('ab', 'abc')).toBeLessThan(0);
    expect(byteCompare('same', 'same')).toBe(0);
    // U+FFFD sorts before U+1F600 in bytes, after it in UTF-16 units.
    expect(byteCompare('�', '😀')).toBeLessThan(0);
    expect('�' < '😀').toBe(false);
  });

  it('hints at more than 16 characters, § and control characters', () => {
    expect(prefixProblem('')).toBe(false);
    expect(prefixProblem('[R]')).toBe(false);
    expect(prefixProblem('x'.repeat(16))).toBe(false);
    expect(prefixProblem('😀'.repeat(16))).toBe(false);
    expect(prefixProblem('x'.repeat(17))).toBe(true);
    expect(prefixProblem('§a')).toBe(true);
    expect(prefixProblem('a‮b')).toBe(true);
    expect(prefixProblem('a\nb')).toBe(true);
  });

  const groups = [
    { id: 'b', name: 'same', server_ids: ['s'] },
    { id: 'a', name: 'same', server_ids: ['s'] },
    { id: 'c', name: 'Alpha', server_ids: ['s'] },
    { id: 'd', name: 'other', server_ids: ['t'] },
  ];

  it('takes the first group by name, then by ID, that has a prefix', () => {
    expect(winningGroup('s', groups, { a: '[A]', b: '[B]', c: '[C]' })?.id).toBe('c');
    expect(winningGroup('s', groups, { a: '[A]', b: '[B]', c: '' })?.id).toBe('a');
    expect(winningGroup('s', groups, { a: '', b: '[B]', c: '' })?.id).toBe('b');
    expect(winningGroup('s', groups, { a: '', b: '', c: '' })).toBeNull();
  });

  it('gives no answer while an earlier group is unknown', () => {
    expect(winningGroup('s', groups, { a: '[A]', b: '[B]' })).toBeUndefined();
    // An unknown group after the winner does not matter.
    expect(winningGroup('s', groups, { c: '[C]' })?.id).toBe('c');
  });
});
