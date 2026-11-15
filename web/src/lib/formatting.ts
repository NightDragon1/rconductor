// SPDX-License-Identifier: AGPL-3.0-or-later

// Formatting codes in game server answers (CMD-017). The profile field
// formatting names the scheme; minecraft turns § codes into styles of the
// fixed list below. The class of a segment is always one of the strings in
// this file, never a value from the answer: a code only picks an entry
// (SEC-006). Unknown codes stay in the text, as with the flag off.

export type Formatting = 'minecraft';

// The flag name the backend reports in ui.features.
export const COLOR_CODES_FLAG = 'color_codes';

// One Minecraft color per code, as a theme token with a light and a dark
// value of at least 4.5:1 on every surface (index.css).
const colorClass: Record<string, string> = {
  '0': 'text-mc-0',
  '1': 'text-mc-1',
  '2': 'text-mc-2',
  '3': 'text-mc-3',
  '4': 'text-mc-4',
  '5': 'text-mc-5',
  '6': 'text-mc-6',
  '7': 'text-mc-7',
  '8': 'text-mc-8',
  '9': 'text-mc-9',
  a: 'text-mc-a',
  b: 'text-mc-b',
  c: 'text-mc-c',
  d: 'text-mc-d',
  e: 'text-mc-e',
  f: 'text-mc-f',
};

// Underline and strikethrough share one CSS property, so the pair has an
// entry of its own.
const decorationClass = {
  '': '',
  u: 'underline',
  s: 'line-through',
  us: '[text-decoration-line:underline_line-through]',
} as const;

// MAX_STYLED caps the styled segments of one text: a long answer full of
// codes stays cheap to show; after the cap the rest is shown as with the
// flag off, codes removed.
export const MAX_STYLED = 2000;

export type Segment = { text: string; className: string };

type Style = { color: string; bold: boolean; italic: boolean; under: boolean; strike: boolean };
const plain = (): Style => ({ color: '', bold: false, italic: false, under: false, strike: false });

function classOf(s: Style): string {
  const deco =
    decorationClass[`${s.under ? 'u' : ''}${s.strike ? 's' : ''}` as keyof typeof decorationClass];
  return [s.color, s.bold && 'font-bold', s.italic && 'italic', deco].filter(Boolean).join(' ');
}

const hexDigit = /^[0-9a-f]$/i;
const stripRest = (t: string) => t.replace(/§[0-9a-fk-orx]/gi, '');

// isHexColor reports §x§R§R§G§G§B§B at i: free colors are not in the fixed
// list, the whole sequence is dropped.
function isHexColor(text: string, i: number): boolean {
  for (let k = 0; k < 6; k++) {
    const at = i + 2 + 2 * k;
    if (text[at] !== '§' || !hexDigit.test(text[at + 1] ?? '')) return false;
  }
  return true;
}

// formatLines cuts the text into lines and each line into segments of one
// style. Styles end at a line break, as each line of an answer stands
// alone; §r and a color reset them, a color also ends bold, italic,
// underline and strikethrough (the game does the same). §k (obfuscated)
// has no style here and is dropped.
export function formatLines(text: string): Segment[][] {
  let budget = MAX_STYLED;
  return text.split('\n').map((line) => {
    if (budget <= 0) return [{ text: stripRest(line), className: '' }];
    const out: Segment[] = [];
    let style = plain();
    let buf = '';
    const flush = () => {
      if (!buf) return;
      const className = classOf(style);
      const last = out[out.length - 1];
      if (last && last.className === className) last.text += buf;
      else {
        out.push({ text: buf, className });
        if (className) budget--;
      }
      buf = '';
    };
    for (let i = 0; i < line.length; i++) {
      const ch = line[i];
      // ASCII only, before lowercasing: U+212A would turn into k.
      const next = line[i + 1] ?? '';
      const code = ch === '§' && /^[0-9a-fk-orx]$/i.test(next) ? next.toLowerCase() : '';
      if (!code) {
        buf += ch;
        continue;
      }
      flush();
      if (budget <= 0) {
        out.push({ text: stripRest(line.slice(i)), className: '' });
        return out;
      }
      if (code === 'x') {
        if (isHexColor(line, i)) i += 12;
        style = plain();
      } else if (colorClass[code]) style = { ...plain(), color: colorClass[code] };
      else if (code === 'l') style = { ...style, bold: true };
      else if (code === 'o') style = { ...style, italic: true };
      else if (code === 'n') style = { ...style, under: true };
      else if (code === 'm') style = { ...style, strike: true };
      else if (code === 'r') style = plain();
      i++;
    }
    flush();
    return out;
  });
}
