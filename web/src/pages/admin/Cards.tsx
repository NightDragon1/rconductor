// SPDX-License-Identifier: AGPL-3.0-or-later
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

export interface Fact {
  label: string;
  value: ReactNode;
  className?: string;
}

// Cards stand in for a table of the administration on a phone (UI-012):
// nothing is cut off and nothing scrolls sideways.
export function Cards({
  label,
  empty,
  children,
}: {
  label: string;
  empty?: string;
  children: ReactNode;
}) {
  return (
    <ul aria-label={label} className="flex flex-col gap-2">
      {children}
      {empty && <li className="p-4 text-center text-muted">{empty}</li>}
    </ul>
  );
}

// Card shows one entry; where there are details, the whole card opens them.
export function Card({
  title,
  mono = false,
  selected = false,
  onOpen,
  facts,
}: {
  title: ReactNode;
  mono?: boolean;
  selected?: boolean;
  onOpen?: () => void;
  facts: Fact[];
}) {
  const heading = cn('font-semibold wrap-anywhere', mono && 'font-mono');
  return (
    <li
      className={cn(
        'relative flex flex-col gap-1 rounded-lg border border-border bg-surface p-3',
        onOpen && 'hover:border-control hover:bg-surface-raised',
        selected && 'bg-surface-raised',
      )}
    >
      {onOpen ? (
        <button
          type="button"
          className={cn('min-h-11 text-left after:absolute after:inset-0', heading)}
          onClick={onOpen}
        >
          {title}
        </button>
      ) : (
        <p className={heading}>{title}</p>
      )}
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm">
        {facts.map((f) => (
          <div key={f.label} className="contents">
            <dt className="text-muted">{f.label}</dt>
            <dd className={cn('wrap-anywhere', f.className)}>{f.value}</dd>
          </div>
        ))}
      </dl>
    </li>
  );
}
