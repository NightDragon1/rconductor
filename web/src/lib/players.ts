// SPDX-License-Identifier: AGPL-3.0-or-later
import type { components } from '@/api/schema';

type PlayerList = components['schemas']['PlayerList'];

// playerCount is what the interface shows of a player list (PROT-003): the
// count (from the backend, or the number of names), the limit if the game
// tells it, and whether the game gives no names although players are on.
export function playerCount(p: PlayerList | undefined): {
  count: number;
  max?: number;
  noNames: boolean;
} {
  const names = p?.players.length ?? 0;
  const count = p?.count ?? names;
  return { count, max: p?.max, noNames: !!p?.available && names === 0 && count > 0 };
}
