// SPDX-License-Identifier: AGPL-3.0-or-later

// Bidi controls can reorder what a reader sees. The backend keeps them in
// free text, the interface shows them instead of letting them act
// (revision 2.4 candidate 28, SEC-006).
const bidiControl = /([\u061C\u200E\u200F\u202A-\u202E\u2066-\u2069])/;

// splitBidi cuts a line at its bidi controls: text at even places, one
// control at each odd place.
export function splitBidi(line: string): string[] {
  return line.split(bidiControl);
}

// bidiLabel is the visible stand-in of a control.
export function bidiLabel(control: string): string {
  const code = (control.codePointAt(0) ?? 0).toString(16).toUpperCase().padStart(4, '0');
  return `⟨U+${code}⟩`;
}
