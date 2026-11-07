// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { formatDuration, isGoDuration, parseDuration } from './duration';

describe('durations of the settings', () => {
  it('shows Go durations in the largest exact unit', () => {
    expect(formatDuration('72h0m0s')).toBe('72 h');
    expect(formatDuration('15m0s')).toBe('15 min');
    expect(formatDuration('1h30m0s')).toBe('90 min');
    expect(formatDuration('30s')).toBe('30 s');
    expect(formatDuration('168h0m0s')).toBe('168 h');
  });

  it('reads what people type', () => {
    expect(parseDuration('72 h')).toBe('72h');
    expect(parseDuration('72h')).toBe('72h');
    expect(parseDuration('15 min')).toBe('15m');
    expect(parseDuration(' 30 S ')).toBe('30s');
    expect(parseDuration('2 hours')).toBe('2h');
    expect(parseDuration('1,5 h')).toBe('5400s');
    expect(parseDuration('90')).toBeNull();
    expect(parseDuration('-5 min')).toBeNull();
    expect(parseDuration('5 days')).toBeNull();
    expect(parseDuration('0 h')).toBeNull();
  });

  it('round-trips', () => {
    for (const go of ['72h0m0s', '15m0s', '12h0m0s', '30s']) {
      const back = parseDuration(formatDuration(go));
      expect(back && formatDuration(back)).toBe(formatDuration(go));
    }
  });

  it('tells durations from other values', () => {
    expect(isGoDuration('72h0m0s')).toBe(true);
    expect(isGoDuration('30s')).toBe(true);
    expect(isGoDuration('mods')).toBe(false);
    expect(isGoDuration(5)).toBe(false);
  });
});
