// SPDX-License-Identifier: AGPL-3.0-or-later
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { useServerGroups, useServers } from '@/api/servers';
import { useMultiCommand, type MultiResult } from '@/api/multi';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { stripCodes } from '@/lib/serverpage';

const tone: Record<MultiResult['items'][number]['result'], string> = {
  ok: 'text-ok',
  denied: 'text-danger',
  error: 'text-danger',
  timeout: 'text-warn',
  not_found: 'text-danger',
};

// MultiCommand sends one command to several servers of the caller's scope
// (CMD-010, D-069); the panel checks it per server and answers per server.
export function MultiCommand({
  current,
  open,
  onClose,
}: {
  current: string;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const servers = useServers();
  const groups = useServerGroups(open);
  const send = useMultiCommand();
  const [picked, setPicked] = useState<Set<string>>(() => new Set([current]));
  const [command, setCommand] = useState('');
  const commandId = useId();
  const all = [...(servers.data ?? [])].sort((a, b) => a.name.localeCompare(b.name));
  const name = (id: string) => all.find((s) => s.id === id)?.name ?? id;
  const toggle = (id: string) =>
    setPicked((p) => {
      const n = new Set(p);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!command.trim() || picked.size === 0 || send.isPending) return;
    send.mutate({ serverIds: [...picked], command: command.trim() });
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogTitle>{t('multi.title')}</DialogTitle>
        <DialogDescription>{t('multi.explain')}</DialogDescription>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-sm text-muted">{t('multi.servers')}</legend>
            {(groups.data ?? []).length > 0 && (
              <div data-scrolls className="flex flex-wrap gap-2">
                {(groups.data ?? []).map((g) => (
                  <Button
                    key={g.id}
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      setPicked(
                        (p) =>
                          new Set([
                            ...p,
                            ...g.server_ids.filter((id) => all.some((s) => s.id === id)),
                          ]),
                      )
                    }
                  >
                    {t('multi.addGroup', { name: g.name })}
                  </Button>
                ))}
              </div>
            )}
            <ul className="flex flex-col">
              {all.map((s) => (
                <li key={s.id}>
                  <label className="flex min-h-11 items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="max-md:size-11"
                      checked={picked.has(s.id)}
                      onChange={() => toggle(s.id)}
                    />
                    <bdi>
                      <ForeignText text={s.name} />
                    </bdi>
                  </label>
                </li>
              ))}
            </ul>
          </fieldset>
          <div className="flex flex-col gap-1">
            <label htmlFor={commandId} className="text-sm text-muted">
              {t('multi.command')}
            </label>
            <Input
              id={commandId}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              value={command}
              onChange={(e) => setCommand(e.target.value)}
            />
          </div>
          <Button
            type="submit"
            className="self-start max-md:self-stretch"
            disabled={!command.trim() || picked.size === 0 || send.isPending}
          >
            {t('multi.send', { count: picked.size })}
          </Button>
        </form>
        {send.isError && (
          <p role="alert" className="text-sm text-danger">
            {reasonText(t, send.error instanceof ApiError ? send.error.problem : undefined)}
          </p>
        )}
        {send.data && (
          <section aria-label={t('multi.results')}>
            <h3 className="mb-2 text-sm font-semibold">{t('multi.results')}</h3>
            <ul className="flex flex-col gap-2">
              {send.data.items.map((it) => (
                <li
                  key={it.server_id}
                  aria-label={name(it.server_id)}
                  className="rounded-md border border-border p-2 text-sm"
                >
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <bdi className="font-semibold">
                      <ForeignText text={name(it.server_id)} />
                    </bdi>
                    <span className={tone[it.result]}>
                      {t(`multi.result.${it.result}`)}
                      {it.reason && it.result !== 'not_found' && (
                        <span className="ml-1 font-mono text-xs">({it.reason})</span>
                      )}
                    </span>
                  </div>
                  {it.response && (
                    <pre className="mt-1 font-mono text-xs whitespace-pre-wrap text-muted">
                      <ForeignText text={stripCodes(it.response)} />
                    </pre>
                  )}
                </li>
              ))}
            </ul>
          </section>
        )}
      </DialogContent>
    </Dialog>
  );
}
