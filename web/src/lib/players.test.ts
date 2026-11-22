// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { playerCount, suggestable } from './players';

const list = (o: object) => ({ available: true, players: [], stale: false, ...o });

describe('playerCount (PROT-003)', () => {
  it('takes count and max of a game without names', () => {
    expect(playerCount(list({ count: 3, max: 8 }))).toEqual({ count: 3, max: 8, noNames: true });
  });
  it('says nothing about names while nobody is on', () => {
    expect(playerCount(list({ count: 0, max: 8 }))).toEqual({ count: 0, max: 8, noNames: false });
  });
  it('counts names when the backend gives no count', () => {
    expect(playerCount(list({ players: ['Ada', 'Bob'] }))).toEqual({
      count: 2,
      max: undefined,
      noNames: false,
    });
  });
  it('is empty without a list', () => {
    expect(playerCount(undefined)).toEqual({ count: 0, max: undefined, noNames: false });
    expect(playerCount({ available: false, players: [], stale: false })).toEqual({
      count: 0,
      max: undefined,
      noNames: false,
    });
  });
});

describe('suggestable (CMD-004)', () => {
  it('offers plain names', () => {
    expect(['Steve', 'alex_99', 'Ünal-2', 'x.y'].every(suggestable)).toBe(true);
  });
  it('leaves out names with spaces, quotes or separators', () => {
    for (const n of [
      '',
      'two words',
      'a;stop',
      'a|b',
      'a&b',
      'a"b',
      "a'b",
      'a`b',
      'a\\b',
      'tab\tname',
      'new\nline',
    ]) {
      expect(suggestable(n), n).toBe(false);
    }
  });
});
