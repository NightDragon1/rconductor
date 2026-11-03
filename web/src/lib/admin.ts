// SPDX-License-Identifier: AGPL-3.0-or-later
import type { CommandListEntry } from '@/api/users';

// Reason codes with their own text (Design 3.5).
const known = ['forbidden', 'not_found', 'invalid', 'rate_limited', 'busy', 'conflict'];

export function reasonText(t: (k: string) => string, reason?: string): string {
  return t(known.includes(reason ?? '') ? `admin.reason.${reason}` : 'admin.reason.other');
}

// lockedOk: at least one command of a list stays locked for supporters
// (ROLE-007); the backend refuses the list otherwise.
export const lockedOk = (entries: CommandListEntry[]) =>
  entries.length === 0 || entries.some((e) => !e.passable);
