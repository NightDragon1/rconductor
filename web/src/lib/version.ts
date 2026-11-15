// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// majorVersion is the number before the first dot of a version, a leading
// "v" ignored; undefined when that part is not a number, as for "unknown"
// (UI-024).
export function majorVersion(version: string): number | undefined {
  const head = version.trim().replace(/^v/i, '').split('.')[0] ?? '';
  return /^\d+$/.test(head) ? Number(head) : undefined;
}

// majorDiffers tells whether an agent runs another major version than the
// panel; without a number on either side there is nothing to compare.
export function majorDiffers(agent: string, panel: string): boolean {
  const a = majorVersion(agent);
  const p = majorVersion(panel);
  return a !== undefined && p !== undefined && a !== p;
}
