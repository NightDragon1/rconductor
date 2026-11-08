// SPDX-License-Identifier: AGPL-3.0-or-later
import type { components } from '@/api/schema';
import type { Param, Values } from '@/lib/commandform';

export type Template = components['schemas']['Template'];

// A placeholder is {name} with a lower-case name, as in the backend
// (CMD-008); other braces are text.
const placeholder = /\{([a-z][a-z0-9_]{0,31})\}/g;

// templateParams gives one field per placeholder: {player} picks a name
// from the player list, every other one is text.
export function templateParams(t: Template): Param[] {
  return t.placeholders.map((name) => ({ name, type: name === 'player' ? 'player' : 'text' }));
}

// fillTemplate puts the values in; the panel checks the command like a
// typed one (D-068). Line breaks become spaces, the pipeline would drop
// them anyway (CMD-006).
export function fillTemplate(text: string, values: Values): string {
  return text
    .replace(placeholder, (_, name: string) => (values[name] ?? '').replace(/\s+/g, ' ').trim())
    .trim();
}

// matchesTemplate tells whether a template fits the search text.
export function matchesTemplate(t: Template, text: string): boolean {
  const q = text.trim().toLowerCase();
  return !q || t.name.toLowerCase().includes(q) || t.template.toLowerCase().includes(q);
}
