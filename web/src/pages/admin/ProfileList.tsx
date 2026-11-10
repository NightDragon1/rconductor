// SPDX-License-Identifier: AGPL-3.0-or-later
import { ChevronDown, ChevronUp, GripVertical, Minus } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { components } from '@/api/schema';
import { addProfile, addable, genericAboveSpecific, moveProfile } from '@/lib/profiles';
import { cn } from '@/lib/utils';

type Profile = components['schemas']['GameProfile'];

// ProfileList edits the ordered game profiles of a server, top first
// (SRV-008, CAT-007, D-074): "+" adds one of the list's protocol, "−"
// removes, drag and drop or the arrow buttons (keyboard, touch) reorder.
// It only edits the draft; save sends the list and shows the backend's.
export function ProfileList({
  ids,
  profiles,
  disabled,
  onChange,
}: {
  ids: string[];
  profiles: Profile[];
  disabled: boolean;
  onChange: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const headId = useId();
  const pickId = useId();
  const list = useRef<HTMLOListElement>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const [over, setOver] = useState<number | null>(null);
  // The button that keeps the focus after a move by keyboard.
  const focus = useRef<{ id: string; dir: 'up' | 'down' } | null>(null);
  useEffect(() => {
    const f = focus.current;
    if (!f || !list.current) return;
    focus.current = null;
    const pick = (dir: string) =>
      list.current?.querySelector<HTMLButtonElement>(
        `[data-move="${dir}"][data-profile="${CSS.escape(f.id)}"]`,
      );
    const b = pick(f.dir);
    (b && !b.disabled ? b : pick(f.dir === 'up' ? 'down' : 'up'))?.focus();
  }, [ids]);

  const profile = (id: string) => profiles.find((p) => p.id === id);
  const label = (p: Profile) =>
    p.kind === 'specific' ? t('servers.specificProfile', { name: p.name }) : p.name;
  const nameOf = (id: string) => profile(id)?.name ?? id;
  const move = (i: number, dir: 'up' | 'down') => {
    const id = ids[i];
    if (id === undefined) return;
    focus.current = { id, dir };
    onChange(moveProfile(ids, i, dir === 'up' ? i - 1 : i + 1));
  };
  const end = () => {
    setDrag(null);
    setOver(null);
  };
  const options = addable(ids, profiles);
  const button =
    'inline-flex size-8 items-center justify-center rounded-md text-muted hover:bg-surface-raised hover:text-text disabled:opacity-40 disabled:hover:bg-transparent';

  return (
    <div className="flex flex-col gap-1">
      <span id={headId} className="text-sm text-muted">
        {t('servers.profiles')}
      </span>
      {ids.length === 0 ? (
        <p className="text-sm text-muted">{t('servers.noProfile')}</p>
      ) : (
        <ol ref={list} aria-labelledby={headId} className="flex flex-col gap-1">
          {ids.map((id, i) => {
            const p = profile(id);
            return (
              <li
                key={id}
                // A sortable list entry, the one exception to "nothing can be
                // dragged" (UI-001, D-075); it also has the arrow buttons.
                data-sortable-list=""
                draggable={!disabled}
                onDragStart={(e) => {
                  setDrag(i);
                  e.dataTransfer.effectAllowed = 'move';
                  e.dataTransfer.setData('text/plain', id);
                }}
                onDragOver={(e) => {
                  if (drag === null) return;
                  e.preventDefault();
                  e.dataTransfer.dropEffect = 'move';
                  setOver(i);
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  if (drag !== null) onChange(moveProfile(ids, drag, i));
                  end();
                }}
                onDragEnd={end}
                data-testid="profile-item"
                className={cn(
                  'flex items-center gap-1 rounded-md border border-border px-2 py-1 text-sm',
                  !disabled && 'cursor-grab',
                  drag === i && 'opacity-50',
                  over === i && drag !== i && 'border-accent',
                )}
              >
                {!disabled && <GripVertical aria-hidden className="size-4 shrink-0 text-muted" />}
                <span className="min-w-0 flex-1 wrap-anywhere">
                  <bdi>{p ? label(p) : id}</bdi>
                </span>
                {!disabled && (
                  <>
                    <button
                      type="button"
                      className={button}
                      data-move="up"
                      data-profile={id}
                      disabled={i === 0}
                      aria-label={t('servers.moveUp', { name: nameOf(id) })}
                      onClick={() => move(i, 'up')}
                    >
                      <ChevronUp aria-hidden className="size-4" />
                    </button>
                    <button
                      type="button"
                      className={button}
                      data-move="down"
                      data-profile={id}
                      disabled={i === ids.length - 1}
                      aria-label={t('servers.moveDown', { name: nameOf(id) })}
                      onClick={() => move(i, 'down')}
                    >
                      <ChevronDown aria-hidden className="size-4" />
                    </button>
                    <button
                      type="button"
                      className={cn(button, 'hover:text-danger')}
                      aria-label={t('admin.removeItem', { name: nameOf(id) })}
                      onClick={() => onChange(ids.filter((x) => x !== id))}
                    >
                      <Minus aria-hidden className="size-4" />
                    </button>
                  </>
                )}
              </li>
            );
          })}
        </ol>
      )}
      {genericAboveSpecific(ids, profiles) && (
        <p role="status" className="text-sm text-warn">
          {t('servers.profileOrder')}
        </p>
      )}
      {!disabled && options.length > 0 && (
        <>
          <label htmlFor={pickId} className="sr-only">
            {t('servers.addProfile')}
          </label>
          <select
            id={pickId}
            className="h-11 self-start rounded-md border border-control bg-surface px-3 text-sm"
            value=""
            onChange={(e) => e.target.value && onChange(addProfile(ids, e.target.value, profiles))}
          >
            <option value="">{t('servers.addProfileOption')}</option>
            {options.map((p) => (
              <option key={p.id} value={p.id}>
                {label(p)}
              </option>
            ))}
          </select>
        </>
      )}
      {!disabled && ids.length > 1 && (
        <p className="text-xs text-muted">{t('servers.profilesHint')}</p>
      )}
    </div>
  );
}
