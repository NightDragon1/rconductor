// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// The flag name the backend reports in ui.features (CMD-020).
export const SAY_SENDER_FLAG = 'say_sender';

// The bound of a prefix as the backend checks it (CMD-020, D-082).
export const MAX_SAY_PREFIX = 16;

const encoder = new TextEncoder();

// byteCompare orders two strings by their UTF-8 bytes, as the backend
// sorts group names (D-082); the order of JavaScript strings (UTF-16)
// differs above U+FFFF.
export function byteCompare(a: string, b: string): number {
  const x = encoder.encode(a);
  const y = encoder.encode(b);
  const n = Math.min(x.length, y.length);
  for (let i = 0; i < n; i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0);
    if (d !== 0) return d;
  }
  return x.length - y.length;
}

// prefixProblem is only a hint for the input; the backend decides
// (at most 16 printable characters without §).
export function prefixProblem(v: string): boolean {
  const chars = [...v];
  return chars.length > MAX_SAY_PREFIX || chars.some((c) => c === '§' || /[\p{C}]/u.test(c));
}

export interface PrefixGroup {
  id: string;
  name: string;
  server_ids: string[];
}

// winningGroup is the group whose prefix a server uses: the first of its
// groups by name in byte order, then by ID, that has a prefix (D-082).
// prefixes maps a group ID to its prefix; undefined means unknown, and
// then no answer is given: an unknown group might be the first.
export function winningGroup(
  serverId: string,
  groups: PrefixGroup[],
  prefixes: Record<string, string | undefined>,
): PrefixGroup | null | undefined {
  const own = groups
    .filter((g) => g.server_ids.includes(serverId))
    .sort((a, b) => byteCompare(a.name, b.name) || byteCompare(a.id, b.id));
  for (const g of own) {
    const p = prefixes[g.id];
    if (p === undefined) return undefined;
    if (p.trim() !== '') return g;
  }
  return null;
}
