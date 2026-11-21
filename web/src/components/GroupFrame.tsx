// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { ReactNode } from 'react';

// GroupFrame puts content together under a head line (UI-021): no full
// border, only two corner brackets, bottom left and top right, all frames
// in one colour; groups differ by their name. It knows nothing of what it
// groups, so hosts, server groups or zones can use it alike.
export function GroupFrame({
  name,
  title,
  summary,
  children,
}: {
  // name labels the region; title replaces it in the head when given.
  name: string;
  title?: ReactNode;
  summary?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section aria-label={name} className="relative p-4">
      <span
        aria-hidden
        className="pointer-events-none absolute top-0 right-0 size-6 rounded-tr-xl border-t-2 border-r-2 border-frame"
      />
      <span
        aria-hidden
        className="pointer-events-none absolute bottom-0 left-0 size-6 rounded-bl-xl border-b-2 border-l-2 border-frame"
      />
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 className="font-mono text-sm font-semibold text-frame uppercase">
          {title ?? <bdi>{name}</bdi>}
        </h2>
        {summary !== undefined && <span className="text-sm text-muted">{summary}</span>}
      </div>
      {children}
    </section>
  );
}
