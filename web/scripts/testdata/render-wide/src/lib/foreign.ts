// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
// Stand-in of the cleaning function of the interface (fitness test data).
export function plainText(text: string): string {
  return text;
}

// Leaves the text as it is: no cleaning, whatever name it is imported as.
export function rawText(text: string): string {
  return text;
}
