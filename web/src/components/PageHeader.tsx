// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { ReactNode } from 'react';

// PageHeader is the heading of a page with its one-line description in
// grey below; children add further lines (counts, notes), actions sit on
// the right of the heading.
export function PageHeader({
  title,
  description,
  actions,
  children,
}: {
  title: ReactNode;
  description: string;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  const head = (
    <div>
      <h1 className="text-2xl font-semibold">{title}</h1>
      <p className="text-sm text-muted">{description}</p>
      {children}
    </div>
  );
  if (!actions) return head;
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      {head}
      {actions}
    </div>
  );
}
