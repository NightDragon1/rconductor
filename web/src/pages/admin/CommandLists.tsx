// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import { useProfiles } from '@/api/servers';
import { commandListsKey, outcome, useCommandLists, type CommandList } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useShortcuts } from '@/lib/shortcuts';
import { useUndo } from '@/lib/undo';
import { cn } from '@/lib/utils';
import { lockedOk, reasonText } from '@/lib/admin';

function ListPanel({ list, onClose }: { list: CommandList; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { schedule } = useUndo();
  const profiles = useProfiles();
  const [draft, setDraft] = useState<CommandList>(list);
  const [path, setPath] = useState('');
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [removing, setRemoving] = useState(false);
  const pathId = useId();
  const catalogId = useId();
  const catalog = [
    ...new Set((profiles.data ?? []).flatMap((p) => p.catalog.map((c) => c.path))),
  ].sort();

  // Edits change the draft at once and save it as a whole after the undo
  // time; undo goes back to the saved list.
  const change = (next: CommandList, label: string) => {
    setDraft(next);
    if (!lockedOk(next.entries)) {
      setMessage({ ok: false, text: t('lists.oneLocked') });
      return;
    }
    setMessage(null);
    schedule(
      label,
      async (keepalive) => {
        const r = await outcome(list.id, async () =>
          must(
            await api.PATCH('/command-lists/{id}', {
              params: { path: { id: list.id } },
              body: { name: next.name, entries: next.entries },
              keepalive,
            }),
          ),
        );
        setMessage(
          r.ok
            ? { ok: true, text: t('admin.saved') }
            : { ok: false, text: reasonText(t, r.reason) },
        );
        void qc.invalidateQueries({ queryKey: commandListsKey });
      },
      { key: `list:${list.id}`, onUndo: () => setDraft(list) },
    );
  };
  const add = (e: FormEvent) => {
    e.preventDefault();
    const p = path.trim().replace(/\s+/g, ' ');
    if (!p || draft.entries.some((x) => x.path === p)) return;
    setPath('');
    change(
      { ...draft, entries: [...draft.entries, { path: p, passable: false }] },
      t('lists.undo.changed', { name: draft.name }),
    );
  };

  return (
    <aside
      aria-label={t('admin.panel', { name: draft.name })}
      className="flex w-full flex-col gap-4 rounded-lg border border-border bg-surface p-5 xl:w-[28rem]"
    >
      <div className="flex items-start justify-between gap-3">
        <h2 className="truncate text-lg font-semibold">
          <bdi>{draft.name}</bdi>
        </h2>
        <Button variant="ghost" size="sm" aria-label={t('admin.closePanel')} onClick={onClose}>
          <X aria-hidden className="size-4" />
        </Button>
      </div>
      <p className="text-sm text-muted">{t('lists.explain')}</p>
      <table className="w-full text-sm">
        <thead className="text-left text-muted">
          <tr>
            <th className="py-2 font-medium">{t('lists.path')}</th>
            <th className="py-2 font-medium">{t('lists.passable')}</th>
            <th className="w-8" />
          </tr>
        </thead>
        <tbody>
          {draft.entries.map((e, i) => (
            <tr key={e.path} className="border-t border-border">
              <td className="py-2 font-mono">
                <bdi>{e.path}</bdi>
              </td>
              <td className="py-2">
                <input
                  type="checkbox"
                  aria-label={t('lists.passableFor', { path: e.path })}
                  checked={e.passable}
                  onChange={(ev) =>
                    change(
                      {
                        ...draft,
                        entries: draft.entries.map((x, j) =>
                          j === i ? { ...x, passable: ev.target.checked } : x,
                        ),
                      },
                      t('lists.undo.changed', { name: draft.name }),
                    )
                  }
                />
              </td>
              <td className="py-2">
                <button
                  type="button"
                  className="text-muted hover:text-danger"
                  aria-label={t('admin.removeItem', { name: e.path })}
                  onClick={() =>
                    change(
                      { ...draft, entries: draft.entries.filter((_, j) => j !== i) },
                      t('lists.undo.changed', { name: draft.name }),
                    )
                  }
                >
                  <X aria-hidden className="size-4" />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <form onSubmit={add} className="flex gap-2">
        <label htmlFor={pathId} className="sr-only">
          {t('lists.addLabel')}
        </label>
        <Input
          id={pathId}
          className="font-mono"
          list={catalogId}
          placeholder={t('lists.addPlaceholder')}
          autoComplete="off"
          value={path}
          onChange={(e) => setPath(e.target.value)}
        />
        <datalist id={catalogId}>
          {catalog.map((c) => (
            <option key={c} value={c} />
          ))}
        </datalist>
        <Button type="submit" variant="outline">
          {t('lists.add')}
        </Button>
      </form>
      {message && (
        <p
          role={message.ok ? 'status' : 'alert'}
          className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
        >
          {message.text}
        </p>
      )}
      <Button
        variant="outline"
        className="self-start border-danger text-danger"
        onClick={() => setRemoving(true)}
      >
        {t('lists.delete')}
      </Button>
      <ConfirmDialog
        open={removing}
        title={t('lists.deleteTitle', { name: draft.name })}
        text={t('lists.deleteText')}
        action={t('lists.delete')}
        onClose={() => setRemoving(false)}
        onConfirm={async () => {
          setRemoving(false);
          const r = await outcome(list.id, async () =>
            must(await api.DELETE('/command-lists/{id}', { params: { path: { id: list.id } } })),
          );
          void qc.invalidateQueries({ queryKey: commandListsKey });
          if (r.ok) onClose();
          else setMessage({ ok: false, text: reasonText(t, r.reason) });
        }}
      />
    </aside>
  );
}

// CommandListsPage manages the command lists and the passable commands
// (ROLE-005, ROLE-007): all for the RCON admin, his own for a server
// admin; the backend decides.
export function CommandListsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const lists = useCommandLists(true);
  const [q, setQ] = useState('');
  const [open, setOpen] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const nameId = useId();
  useShortcuts({ Escape: () => setOpen(null) });
  const all = lists.data ?? [];
  const needle = q.trim().toLowerCase();
  const shown = all.filter(
    (l) =>
      !needle ||
      l.name.toLowerCase().includes(needle) ||
      l.entries.some((e) => e.path.includes(needle)),
  );
  const current = all.find((l) => l.id === open);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      const l = must(
        await api.POST('/command-lists', { body: { name: name.trim(), entries: [] } }),
      );
      setName('');
      await qc.invalidateQueries({ queryKey: commandListsKey });
      setOpen(l.id);
    } catch (err) {
      setError(reasonText(t, (err as { problem?: { reason?: string } }).problem?.reason));
    }
  };

  return (
    <div className="flex flex-col gap-4 xl:flex-row xl:items-start">
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div>
          <h1 className="text-2xl font-semibold">{t('nav.commandLists')}</h1>
          <p className="text-sm text-muted">{t('lists.summary', { count: all.length })}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Input
            type="search"
            className="w-full sm:w-72"
            placeholder={t('lists.search')}
            aria-label={t('lists.searchLabel')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <form onSubmit={create} className="flex gap-2">
            <label htmlFor={nameId} className="sr-only">
              {t('lists.newName')}
            </label>
            <Input
              id={nameId}
              placeholder={t('lists.newName')}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <Button type="submit" disabled={!name.trim()}>
              {t('lists.create')}
            </Button>
          </form>
        </div>
        {error && (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        )}
        <div className="overflow-x-auto rounded-lg border border-border bg-surface">
          <table className="w-full text-sm">
            <thead className="text-left text-muted">
              <tr>
                <th className="p-3 font-medium">{t('lists.name')}</th>
                <th className="p-3 font-medium">{t('lists.entries')}</th>
                <th className="p-3 font-medium">{t('lists.passable')}</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((l) => (
                <tr
                  key={l.id}
                  className={cn('border-t border-border', open === l.id && 'bg-surface-raised')}
                >
                  <td className="p-3">
                    <button type="button" className="hover:underline" onClick={() => setOpen(l.id)}>
                      <bdi>{l.name}</bdi>
                    </button>
                  </td>
                  <td className="max-w-80 truncate p-3 font-mono">
                    <bdi>{l.entries.map((e) => e.path).join(', ')}</bdi>
                  </td>
                  <td className="p-3">
                    {t('lists.passableCount', {
                      count: l.entries.filter((e) => e.passable).length,
                    })}
                  </td>
                </tr>
              ))}
              {shown.length === 0 && (
                <tr>
                  <td colSpan={3} className="p-6 text-center text-muted">
                    {t(lists.isPending ? 'app.loading' : 'lists.none')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
      {current && <ListPanel key={current.id} list={current} onClose={() => setOpen(null)} />}
    </div>
  );
}
