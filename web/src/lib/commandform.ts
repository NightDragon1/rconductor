// SPDX-License-Identifier: AGPL-3.0-or-later
import type { components } from '@/api/schema';

export type Command = components['schemas']['CatalogCommand'];
export type Param = components['schemas']['ProfileParam'];
export type Values = Record<string, string>;

// FREE is the field of a command without catalog entry: the rest of the
// line (Design 7.3).
export const FREE = '__args';

// fromCatalog: a command without catalog entry comes without description
// and parameters.
export const fromCatalog = (c: Command) => c.description !== '';

// pattern shows a command with its parameters, e.g. kick {player} {reason}.
export function pattern(c: Command): string {
  if (!fromCatalog(c)) return `${c.path} …`;
  return [c.path, ...c.params.map((p) => `{${p.name}}`)].join(' ');
}

// fieldError checks one value as the backend does for moderator and
// supporter (revision 2.4 candidate 19); it returns a text key or null.
export function fieldError(p: Param, v: string): string | null {
  const value = v.trim();
  if (value === '') return p.optional ? null : 'form.required';
  switch (p.type) {
    case 'player':
      return /\s/.test(value) ? 'form.oneWord' : null;
    case 'choice':
      return (p.values ?? []).includes(value) ? null : 'form.choice';
    case 'number': {
      if (!/^-?\d+$/.test(value)) return 'form.whole';
      const n = Number(value);
      if ((p.min !== undefined && n < p.min) || (p.max !== undefined && n > p.max)) {
        return 'form.range';
      }
      return null;
    }
    default:
      return null;
  }
}

// formErrors checks all fields; an optional parameter may stay empty only
// if all after it are empty too, as the arguments are positional.
export function formErrors(c: Command, values: Values): Record<string, string> {
  const out: Record<string, string> = {};
  if (!fromCatalog(c)) return out;
  let gap = '';
  for (const p of c.params) {
    const v = (values[p.name] ?? '').trim();
    const e = fieldError(p, v);
    if (e) out[p.name] = e;
    else if (v === '' && !gap) gap = p.name;
    else if (v !== '' && gap) out[gap] = 'form.required';
  }
  return out;
}

// buildCommand puts the path and the values together in the order of the
// parameters; empty optional ones at the end are left out.
export function buildCommand(c: Command, values: Values): string {
  if (!fromCatalog(c)) {
    return [c.path, (values[FREE] ?? '').trim()].filter(Boolean).join(' ');
  }
  const parts = [c.path];
  for (const p of c.params) {
    const v = (values[p.name] ?? '').trim();
    if (v === '') break;
    parts.push(v);
  }
  return parts.join(' ');
}
