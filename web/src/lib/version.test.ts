// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { majorDiffers, majorVersion } from './version';

describe('majorVersion (UI-024)', () => {
  it.each([
    ['1.2.3', 1],
    ['v2.0.1', 2],
    ['V3', 3],
    ['10.0', 10],
    [' 4.1 ', 4],
    ['0.10', 0],
  ])('%s has major %s', (v, want) => {
    expect(majorVersion(v)).toBe(want);
  });

  it.each(['unknown', '', 'v', 'dev.1', '1-beta.2', 'x1.2', '.1', '1e3.0'])(
    '%j has no major',
    (v) => {
      expect(majorVersion(v)).toBeUndefined();
    },
  );

  it('copes with a long input', () => {
    expect(majorVersion('a'.repeat(100_000))).toBeUndefined();
    expect(majorVersion(`7.${'1'.repeat(100_000)}`)).toBe(7);
  });
});

describe('majorDiffers (UI-024)', () => {
  it('warns for another major only', () => {
    expect(majorDiffers('2.0.0', '1.4.0')).toBe(true);
    expect(majorDiffers('v1.0.0', '1.4.0')).toBe(false);
    expect(majorDiffers('1.9', 'v1.0')).toBe(false);
  });

  it('does not warn without a number on either side', () => {
    expect(majorDiffers('unknown', '1.0.0')).toBe(false);
    expect(majorDiffers('1.0.0', 'unknown')).toBe(false);
    expect(majorDiffers('dev', 'dev')).toBe(false);
  });
});
