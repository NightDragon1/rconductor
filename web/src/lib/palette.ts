// SPDX-License-Identifier: AGPL-3.0-or-later
import type { components } from '@/api/schema';
import type { Template } from '@/lib/templates';

// The palette takes the flag name the backend reports in ui.features.
export const PALETTE_FLAG = 'command_palette';

// MAX_RESULTS caps the rendered results: a catalog of thousands of
// commands stays usable, the search narrows it down (CMD-016).
export const MAX_RESULTS = 50;

export type PaletteEntry =
  | { kind: 'command'; key: string; path: string; description: string }
  | { kind: 'template'; key: string; name: string; text: string; placeholders: string[] }
  | { kind: 'favorite'; key: string; id: string; name: string };

// paletteEntries puts together what the backend returned for the caller:
// the allowed commands of the server, its templates and the favorite
// servers in scope. Nothing is added or removed here; rights stay with
// the backend.
export function paletteEntries(
  commands: readonly components['schemas']['CatalogCommand'][],
  templates: readonly Template[],
  favorites: readonly { id: string; name: string }[],
): PaletteEntry[] {
  return [
    ...commands.map((c): PaletteEntry => ({
      kind: 'command',
      key: `c:${c.path}`,
      path: c.path,
      description: c.description,
    })),
    ...templates.map((t): PaletteEntry => ({
      kind: 'template',
      key: `t:${t.id}`,
      name: t.name,
      text: t.template,
      placeholders: t.placeholders,
    })),
    ...favorites.map((s): PaletteEntry => ({
      kind: 'favorite',
      key: `f:${s.id}`,
      id: s.id,
      name: s.name,
    })),
  ];
}

function haystack(e: PaletteEntry): string {
  switch (e.kind) {
    case 'command':
      return `${e.path}\n${e.description}`;
    case 'template':
      return `${e.name}\n${e.text}`;
    case 'favorite':
      return e.name;
  }
}

// searchPalette keeps the entries that hold every word of the query,
// case-insensitive, in their order; at most max of them, plus the number
// of all matches.
export function searchPalette(
  entries: readonly PaletteEntry[],
  query: string,
  max = MAX_RESULTS,
): { shown: PaletteEntry[]; total: number } {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const shown: PaletteEntry[] = [];
  let total = 0;
  for (const e of entries) {
    const h = haystack(e).toLowerCase();
    if (words.every((w) => h.includes(w))) {
      total++;
      if (shown.length < max) shown.push(e);
    }
  }
  return { shown, total };
}
