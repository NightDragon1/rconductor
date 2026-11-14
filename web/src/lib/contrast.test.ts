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
// of the mark reaches 3:1 along all of it (UI-001 with WCAG 1.4.11): the
// mark alone stands for the logo in the collapsed sidebar. The light
// design ends the gradient in a dark cyan for that (scripts/logo.sh).
describe.each([
  ['dark', tokens(dark), 'src/assets/logo/logo-dark.svg', 'src/assets/logo/mark.svg'],
  ['light', tokens(light), 'src/assets/logo/logo.svg', 'src/assets/logo/mark-light.svg'],
])('logo contrast in the %s design', (_, colors, wordMark, mark) => {
  const read = (file: string) => readFileSync(file, 'utf8');
  const solid = [...read(wordMark).matchAll(/<path [^>]*fill="(#[0-9A-Fa-f]{6})"/g)].map(
    (m) => m[1] ?? '',
  );

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

  it.each([
    ['word mark', wordMark],
    ['mark', mark],
  ])('the gradient of the %s reaches 3:1 at every stop and in between', (_part, file) => {
    const gradients = [...read(file).matchAll(/<linearGradient[\s\S]*?<\/linearGradient>/g)].map(
      (g) => [...g[0].matchAll(/stop-color="(#[0-9A-Fa-f]{6})"/g)].map((m) => m[1] ?? ''),
    );
    expect(gradients.length).toBeGreaterThan(0);
    for (const stops of gradients) {
      expect(stops.length).toBeGreaterThan(1);
      // The stops and ten steps between each pair, mixed in sRGB like the
      // browser does by default.
      const measured = stops.flatMap((a, i) => {
        const b = stops[i + 1];
        return b ? Array.from({ length: 10 }, (_, k) => mix(a, b, k / 10)) : [a];
      });
      for (const fg of measured) {
        for (const surface of surfaces) {
          expect(contrast(fg, colors[surface] ?? ''), `${fg} on ${surface}`).toBeGreaterThanOrEqual(
            3,
          );
        }
      }
    }
  });
});

function mix(a: string, b: string, t: number) {
  return (
    '#' +
    [1, 3, 5]
      .map((i) => {
        const [x, y] = [parseInt(a.slice(i, i + 2), 16), parseInt(b.slice(i, i + 2), 16)];
        return Math.round(x + (y - x) * t)
          .toString(16)
          .padStart(2, '0');
      })
      .join('')
  );
}
