// SPDX-License-Identifier: AGPL-3.0-or-later
import { useNavigate } from '@tanstack/react-router';
import { useId, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useAllowedCommands, useFavorites, useServers } from '@/api/servers';
import { useSession } from '@/api/session';
import { useTemplates } from '@/api/templates';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { PALETTE_FLAG, paletteEntries, searchPalette, type PaletteEntry } from '@/lib/palette';
import { usePaletteKey } from '@/lib/shortcuts';
import { cn } from '@/lib/utils';

function Entry({ e }: { e: PaletteEntry }) {
  const { t } = useTranslation();
  return (
    <>
      <span className="w-20 shrink-0 text-xs text-muted">{t(`palette.kind.${e.kind}`)}</span>
      <span className="flex min-w-0 flex-col">
        {e.kind === 'command' && (
          <>
            {/* Commands and their texts come from the game profile (SEC-006). */}
            <code className="font-mono text-sm wrap-anywhere">
              <ForeignText text={e.path} />
            </code>
            {e.description && (
              <span className="truncate text-xs text-muted">
                <ForeignText text={e.description} />
              </span>
            )}
          </>
        )}
        {e.kind === 'template' && (
          <>
            <span className="text-sm wrap-anywhere">
              <ForeignText text={e.name} />
            </span>
            <code className="truncate font-mono text-xs text-muted">
              <ForeignText text={e.text} />
            </code>
          </>
        )}
        {e.kind === 'favorite' && (
          <span className="font-mono text-sm wrap-anywhere">
            <bdi>{e.name}</bdi>
          </span>
        )}
      </span>
    </>
  );
}

// CommandPalette searches the commands, templates and favorites of the
// console (CMD-016), behind the flag command_palette. It shows only what
// the backend returned for the caller; a command picked goes into the
// input line, where the backend checks it like a typed one.
export function CommandPalette({
  serverId,
  onUse,
}: {
  serverId: string;
  onUse: (command: string) => void;
}) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const flagOn = !!session?.ui.features?.includes(PALETTE_FLAG);
  const shortcuts = !!session?.ui.shortcuts_enabled;
  const [open, setOpen] = useState(false);
  usePaletteKey(flagOn, () => setOpen(true));
  if (!flagOn) return null;
  return (
    <>
      <Button
        variant="outline"
        size="sm"
        title={shortcuts ? t('palette.openKey') : undefined}
        onClick={() => setOpen(true)}
      >
        {t('palette.open')}
      </Button>
      {open && <PaletteDialog serverId={serverId} onUse={onUse} onClose={() => setOpen(false)} />}
    </>
  );
}

function PaletteDialog({
  serverId,
  onUse,
  onClose,
}: {
  serverId: string;
  onUse: (command: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const allowed = useAllowedCommands(serverId, !!serverId);
  const templates = useTemplates(serverId);
  const favorites = useFavorites();
  const servers = useServers();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const listId = useId();

  // Favorites are servers to switch to; the current one is no target.
  const favs = (favorites.data ?? []).flatMap((id) => {
    const s = servers.data?.find((x) => x.id === id);
    return s && s.id !== serverId ? [{ id: s.id, name: s.name }] : [];
  });
  const entries = paletteEntries(allowed.data?.commands ?? [], templates.data ?? [], favs);
  const { shown, total } = searchPalette(entries, query);
  const current = Math.min(active, Math.max(shown.length - 1, 0));

  const pick = (e: PaletteEntry) => {
    onClose();
    if (e.kind === 'command') onUse(`${e.path} `);
    else if (e.kind === 'template') onUse(e.text);
    else void navigate({ to: '/servers/$serverId/console', params: { serverId: e.id } });
  };
  const onKey = (ev: KeyboardEvent<HTMLInputElement>) => {
    if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      setActive(Math.min(current + 1, shown.length - 1));
    } else if (ev.key === 'ArrowUp') {
      ev.preventDefault();
      setActive(Math.max(current - 1, 0));
    } else if (ev.key === 'Enter') {
      ev.preventDefault();
      const e = shown[current];
      if (e) pick(e);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl">
        <DialogTitle>{t('palette.title')}</DialogTitle>
        <DialogDescription>{t('palette.explain')}</DialogDescription>
        <Input
          type="search"
          role="combobox"
          aria-expanded
          aria-controls={listId}
          aria-activedescendant={shown.length > 0 ? `${listId}-${current}` : undefined}
          aria-label={t('palette.search')}
          placeholder={t('palette.search')}
          autoComplete="off"
          spellCheck={false}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKey}
        />
        <ul
          id={listId}
          role="listbox"
          aria-label={t('palette.results')}
          data-scrolls
          className="-mx-2 min-h-0 overflow-y-auto"
        >
          {shown.map((e, i) => (
            <li
              key={e.key}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              className={cn(
                'flex min-h-11 cursor-pointer items-center gap-3 rounded-md px-2 py-1.5',
                i === current && 'bg-surface-raised',
              )}
              onMouseMove={() => i !== current && setActive(i)}
              onClick={() => pick(e)}
            >
              <Entry e={e} />
            </li>
          ))}
        </ul>
        {allowed.isSuccess && shown.length === 0 && (
          <p className="text-sm text-muted">{t('palette.none')}</p>
        )}
        {total > shown.length && (
          <p role="status" className="text-xs text-muted">
            {t('palette.more', { shown: shown.length, total })}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}
