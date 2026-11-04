// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { bidiLabel, splitBidi } from './foreign';

describe('splitBidi (SEC-006)', () => {
  it('keeps plain text whole', () => {
    expect(splitBidi('plain שלום text')).toEqual(['plain שלום text']);
  });
  it('puts every control at an odd place', () => {
    expect(splitBidi('a\u202Eb\u2066\u2069')).toEqual([
      'a',
      '\u202E',
      'b',
      '\u2066',
      '',
      '\u2069',
      '',
    ]);
  });
  it('leaves joiners alone, emoji and scripts need them', () => {
    expect(splitBidi('a\u200Db')).toEqual(['a\u200Db']);
  });
});

describe('bidiLabel', () => {
  it('names the code point', () => {
    expect(bidiLabel('\u202E')).toBe('⟨U+202E⟩');
    expect(bidiLabel('\u061C')).toBe('⟨U+061C⟩');
  });
});
