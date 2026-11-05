// SPDX-License-Identifier: AGPL-3.0-or-later

// pseudo turns every text of the English file into a marked test text, so
// a test can find text that does not come from a language file (NFR-007).
export function pseudo(v: unknown): unknown {
  if (typeof v === 'string') return `⟦${v}⟧`;
  if (v && typeof v === 'object') {
    return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, pseudo(x)]));
  }
  return v;
}
