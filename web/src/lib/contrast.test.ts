// SPDX-License-Identifier: AGPL-3.0-or-later
/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// UI-001 with WCAG 1.4.11: symbols, control frames, the focus ring and
// status dots reach 3:1 on every surface, in both designs. The frames of
// cards and tables (--border) only structure the page and are left out:
// the content of a card is recognisable without them.
const css = readFileSync('src/index.css', 'utf8');
const [dark, light] = css.split(":root[data-theme='light']") as [string, string];

function tokens(block: string) {
  return Object.fromEntries(
    [...block.matchAll(/--([a-z0-9-]+): (#[0-9a-f]{6});/g)].map((m) => [m[1], m[2]]),
  ) as Record<string, string>;
}

function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

const surfaces = ['background', 'surface', 'surface-raised'];
const parts: Record<string, string[]> = {
  symbols: ['text', 'text-muted', 'accent', 'ok', 'warn', 'danger'],
  'control frames': ['control-border'],
  'focus ring': ['focus'],
  'status dots': ['ok', 'warn', 'danger', 'text-muted'],
  'group frames': ['group-1', 'group-2', 'group-3', 'group-4', 'group-5', 'group-6'],
};

describe.each([
  ['dark', tokens(dark)],
  ['light', tokens(light)],
])('non-text contrast in the %s design', (_, colors) => {
  it.each(Object.entries(parts))('%s reach 3:1', (_part, names) => {
    for (const name of names) {
      for (const surface of surfaces) {
        const [fg = '', bg = ''] = [colors[name], colors[surface]];
        expect(fg, name).not.toBe('');
        expect(bg, surface).not.toBe('');
        expect(contrast(fg, bg), `${name} on ${surface}`).toBeGreaterThanOrEqual(3);
      }
    }
  });
});

describe.each([
  ['dark', tokens(dark)],
  ['light', tokens(light)],
])('text contrast in the %s design', (_, colors) => {
  it.each(['text', 'text-muted', 'accent', 'ok', 'warn', 'danger'])('%s reaches 4.5:1', (name) => {
    for (const surface of surfaces) {
      const [fg = '', bg = ''] = [colors[name], colors[surface]];
      expect(contrast(fg, bg), `${name} on ${surface}`).toBeGreaterThanOrEqual(4.5);
    }
  });
});

it('measures like the WCAG formula', () => {
  expect(contrast('#000000', '#ffffff')).toBeCloseTo(21);
  expect(contrast('#767676', '#ffffff')).toBeCloseTo(4.54, 1);
});

// UI-023: the logo of each design is readable on every surface of it. The
// words in solid colour reach 4.5:1 like text. The gradient of "RCON" and
// of the mark is a brand colour of the logotype, which WCAG 1.4.3 leaves
// out; its start still reaches 3:1, so the word is recognisable from its
// first letters, and the dark design reaches 3:1 along all of it.
describe.each([
  ['dark', tokens(dark), 'src/assets/logo/logo-dark.svg'],
  ['light', tokens(light), 'src/assets/logo/logo.svg'],
])('logo contrast in the %s design', (theme, colors, file) => {
  const svg = readFileSync(file, 'utf8');
  const solid = [...svg.matchAll(/<path [^>]*fill="(#[0-9A-Fa-f]{6})"/g)].map((m) => m[1] ?? '');
  const stops = [...svg.matchAll(/stop-color="(#[0-9A-Fa-f]{6})"/g)].map((m) => m[1] ?? '');

  it('words in solid colour reach 4.5:1', () => {
    expect(solid.length).toBeGreaterThan(0);
    for (const fg of solid) {
      for (const surface of surfaces) {
        expect(contrast(fg, colors[surface] ?? ''), `${fg} on ${surface}`).toBeGreaterThanOrEqual(
          4.5,
        );
      }
    }
  });

  it('the gradient reaches 3:1 at its start, in the dark design throughout', () => {
    expect(stops.length).toBeGreaterThan(0);
    const measured = theme === 'dark' ? stops : stops.slice(0, 1);
    for (const fg of measured) {
      for (const surface of surfaces) {
        expect(contrast(fg, colors[surface] ?? ''), `${fg} on ${surface}`).toBeGreaterThanOrEqual(
          3,
        );
      }
    }
  });
});
