// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useSyncExternalStore } from 'react';

// From Tailwind's lg: the width where lists become dense tables and pages
// with a list show the details beside it.
export const wideQuery = '(min-width: 64rem)';

const media = () => (typeof window.matchMedia === 'function' ? window.matchMedia(wideQuery) : null);

function subscribe(onChange: () => void) {
  const m = media();
  m?.addEventListener('change', onChange);
  return () => m?.removeEventListener('change', onChange);
}

// useWide tells whether the page has room for the dense layout. It only
// decides what is shown.
export function useWide(): boolean {
  return useSyncExternalStore(subscribe, () => media()?.matches ?? false);
}

// Cells of an administration table: as today below lg, rows of about
// 30 px from lg on.
export const cell = 'p-3 lg:px-3 lg:py-1';
export const headCell = `${cell} font-medium`;
// A button inside a dense row.
export const rowButton = 'lg:h-6 lg:min-h-6 lg:px-2';
