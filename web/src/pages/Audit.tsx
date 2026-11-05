// SPDX-License-Identifier: AGPL-3.0-or-later
import { useInfiniteQuery } from '@tanstack/react-query';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import { useServers } from '@/api/servers';
import { useSession } from '@/api/session';
import { useManagedUsers } from '@/api/users';
import { Button } from '@/components/ui/button';
import { dateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

type Kind = 'command' | 'agent' | 'admin' | 'auth';

const resultTone: Record<string, string> = {
  ok: 'text-ok',
  denied: 'text-danger',
  error: 'text-danger',
  timeout: 'text-warn',
};

// AuditPage shows the audit log with filters (AUD-001): the RCON admin
// all, a server admin his scope; the backend decides. Payloads are shown
// as text.
export function AuditPage() {
  const { t, i18n } = useTranslation();
  const { data: session } = useSession();
  const [kind, setKind] = useState<Kind | ''>('');
  const [server, setServer] = useState('');
  const kindId = useId();
  const serverId = useId();
  const servers = useServers();
  const users = useManagedUsers();
  const log = useInfiniteQuery({
    queryKey: ['audit', kind, server],
    queryFn: async ({ pageParam }) =>
      must(
        await api.GET('/audit', {
          params: {
            query: {
              kind: kind || undefined,
              server_id: server || undefined,
              limit: 50,
              cursor: pageParam,
            },
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const entries = log.data?.pages.flatMap((p) => p.items) ?? [];
  const who = (id?: string) =>
    !id
      ? ''
      : id === session?.user.id
        ? session.user.pseudonym
        : (users.data?.find((u) => u.id === id)?.pseudonym ?? '');
  const serverName = (id?: string) => servers.data?.find((s) => s.id === id)?.name ?? '';
  const select = 'h-9 max-md:h-11 rounded-md border border-control bg-surface px-2 text-sm';

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.audit')}</h1>
        <p className="text-sm text-muted">{t('audit.explain')}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <label htmlFor={kindId} className="sr-only">
          {t('audit.kind')}
        </label>
        <select
          id={kindId}
          className={select}
          value={kind}
          onChange={(e) => setKind(e.target.value as Kind | '')}
        >
          <option value="">{t('audit.allKinds')}</option>
          {(['command', 'agent', 'admin', 'auth'] as const).map((k) => (
            <option key={k} value={k}>
              {t(`audit.kinds.${k}`)}
            </option>
          ))}
        </select>
        <label htmlFor={serverId} className="sr-only">
          {t('audit.server')}
        </label>
        <select
          id={serverId}
          className={select}
          value={server}
          onChange={(e) => setServer(e.target.value)}
        >
          <option value="">{t('audit.allServers')}</option>
          {(servers.data ?? []).map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </div>
      <ol aria-label={t('audit.entries')} className="rounded-lg border border-border bg-surface">
        {entries.map((e) => (
          <li
            key={e.id}
            className="flex flex-col gap-1 border-t border-border px-4 py-2.5 text-sm first:border-t-0"
          >
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <time dateTime={e.ts} className="font-mono text-muted">
                {dateTime(i18n.language, e.ts)}
              </time>
              <span className="rounded border border-border px-1.5 text-xs">
                {t(`audit.kinds.${e.kind}`)}
              </span>
              <code className="font-mono">{e.action}</code>
              {who(e.actor_id) && <bdi className="font-semibold">{who(e.actor_id)}</bdi>}
              {e.actor_role && <span className="text-muted">{t(`role.${e.actor_role}`)}</span>}
              {serverName(e.server_id) && (
                <bdi className="font-mono text-muted">{serverName(e.server_id)}</bdi>
              )}
              <span className={cn('ml-auto', resultTone[e.result] ?? 'text-muted')}>
                {t(`history.result.${e.result}`)}
                {e.reason && <code className="ml-1 font-mono text-xs">({e.reason})</code>}
              </span>
            </div>
            {e.payload !== undefined &&
              e.payload !== null &&
              Object.keys(e.payload as object).length > 0 && (
                <details>
                  <summary className="cursor-pointer text-muted">{t('audit.details')}</summary>
                  <pre className="mt-1 overflow-x-auto rounded bg-surface-raised p-2 font-mono text-xs whitespace-pre-wrap">
                    {JSON.stringify(e.payload, null, 2)}
                  </pre>
                </details>
              )}
          </li>
        ))}
        {log.isSuccess && entries.length === 0 && (
          <li className="p-6 text-center text-muted">{t('audit.none')}</li>
        )}
        {log.isPending && <li className="p-6 text-center text-muted">{t('app.loading')}</li>}
      </ol>
      {log.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={log.isFetchingNextPage}
          onClick={() => void log.fetchNextPage()}
        >
          {t('history.more')}
        </Button>
      )}
    </div>
  );
}
