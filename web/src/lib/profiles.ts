// SPDX-License-Identifier: AGPL-3.0-or-later
// The ordered profile list of a server (SRV-008, CAT-007, D-074): top
// first; a specific profile (modpack, plugin) ranks above a generic one.

export interface ProfileLike {
  id: string;
  kind?: 'generic' | 'specific';
  protocol?: string;
}

// A profile without kind is generic.
const specific = (p?: ProfileLike) => p?.kind === 'specific';

// addProfile inserts a specific profile above the topmost generic one and
// appends a generic one at the end, as the backend sorts (CAT-007).
export function addProfile(ids: string[], id: string, all: ProfileLike[]): string[] {
  if (ids.includes(id)) return ids;
  const byId = (x: string) => all.find((p) => p.id === x);
  if (specific(byId(id))) {
    const at = ids.findIndex((x) => !specific(byId(x)));
    if (at >= 0) return [...ids.slice(0, at), id, ...ids.slice(at)];
  }
  return [...ids, id];
}

// moveProfile moves the entry at from to the index to.
export function moveProfile(ids: string[], from: number, to: number): string[] {
  if (from === to || from < 0 || to < 0 || from >= ids.length || to >= ids.length) return ids;
  const next = [...ids];
  const [x] = next.splice(from, 1);
  next.splice(to, 0, x as string);
  return next;
}

// addable lists the profiles that may join the list: not yet in it and of
// the protocol of the list; the first one may be any.
export function addable<P extends ProfileLike>(ids: string[], all: P[]): P[] {
  const top = all.find((p) => p.id === ids[0]);
  return all.filter((p) => !ids.includes(p.id) && (!top || p.protocol === top.protocol));
}

// genericAboveSpecific tells whether a generic profile stands above a
// specific one (CAT-007).
export function genericAboveSpecific(ids: string[], all: ProfileLike[]): boolean {
  let generic = false;
  for (const id of ids) {
    const p = all.find((x) => x.id === id);
    if (!p) continue;
    if (specific(p)) {
      if (generic) return true;
    } else generic = true;
  }
  return false;
}

// topProfile names the top profile of a server, with "+N" for the others.
export function topProfile(
  t: (key: string, options: { name: string; count: number }) => string,
  ids: string[] | undefined,
  all: { id: string; name: string }[],
): string {
  const name = all.find((p) => p.id === ids?.[0])?.name;
  if (!name || !ids) return '';
  return ids.length > 1 ? t('servers.topProfile', { name, count: ids.length - 1 }) : name;
}
