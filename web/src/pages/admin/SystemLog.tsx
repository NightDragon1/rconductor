// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { plainText } from '@/lib/foreign';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import type { components } from '@/api/schema';
import { useAgents } from '@/api/servers';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { dateTime } from '@/lib/format';
import { cn } from '@/lib/utils';
import { PageHeader } from '@/components/PageHeader';

type Level = components['schemas']['SystemLogLevel'];
type LogPage = components['schemas']['SystemLogPage'];

const levels: Level[] = ['info', 'warn', 'error'];
const periods = { hour: 3_600_000, day: 86_400_000, week: 7 * 86_400_000 } as const;
type Period = keyof typeof periods;

// The page asks for the panel's largest page; the view keeps the newest
// entries only, so a page left open does not grow without end.
const pageSize = 500;
const keep = 2000;
const pollMs = 5000;

const systemLogKey = ['system-log'] as const;

// agentText names the refusals of an agent's log (UI-025): offline (503),
// without the capability (409), unable to read its log (502).
const agentText: Record<number, string> = {
  503: 'systemLog.offline',
  409: 'systemLog.unsupported',
  502: 'systemLog.agentFailed',
};

// failText names a refusal; otherwise the backend's own reason (400, 403,
// 501, ...). The export's 503 means busy and keeps the backend's text.
function failText(t: TFunction, e: unknown, agent = false): string {
  const own = agent && e instanceof ApiError ? agentText[e.status] : undefined;
  if (own) return t(own);
  const p = e instanceof ApiError ? e.problem : undefined;
  const why = p?.detail?.trim() || p?.title?.trim();
  return why ? t('systemLog.failedWith', { reason: why }) : t('systemLog.failed');
}

const field = 'h-9 rounded-md border border-control bg-surface px-2 text-sm max-md:h-11';

// SystemLogPage shows the system log of the panel and, on demand, the log
// of an agent, for the RCON admin only (UI-025, D-091); the export
// downloads the diagnostic package of the chosen period (NFR-018). Lines
// are foreign data and stand as text (SEC-006). The backend checks every
// request.
export function SystemLogPage() {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const agents = useAgents(true);
  const [source, setSource] = useState('panel');
  const [level, setLevel] = useState<Level>('info');
  const [period, setPeriod] = useState<Period>('hour');
  const [busy, setBusy] = useState(false);
  const [exportError, setExportError] = useState('');
  const ids = { source: useId(), level: useId(), period: useId(), notice: useId() };
  const panel = source === 'panel';

  // The panel's log is read once for the period, then polled after the last
  // entry seen; an agent's last lines are read again whole.
  const key = [...systemLogKey, source, level, period];
  const log = useQuery({
    queryKey: key,
    queryFn: async (): Promise<LogPage> => {
      const before = qc.getQueryData<LogPage>(key);
      const last = panel ? before?.items.at(-1)?.id : undefined;
      // from and to from one clock, so 7 days stay within the limit.
      const now = Date.now();
      const query =
        last !== undefined
          ? { source, level, after: last, limit: pageSize }
          : panel
            ? {
                source,
                level,
                from: new Date(now - periods[period]).toISOString(),
                to: new Date(now).toISOString(),
                limit: pageSize,
              }
            : { source, level, limit: pageSize };
      const page = must(await api.GET('/system-log', { params: { query } }));
      if (last === undefined || !before) return page;
      return { ...page, items: [...before.items, ...page.items].slice(-keep) };
    },
    refetchInterval: pollMs,
    retry: false,
  });

  const download = async () => {
    setBusy(true);
    setExportError('');
    try {
      const to = new Date();
      const from = new Date(to.getTime() - periods[period]);
      const blob = must(
        await api.GET('/diagnostics', {
          params: { query: { from: from.toISOString(), to: to.toISOString() } },
          parseAs: 'blob',
        }),
      ) as Blob;
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'rconductor-diagnostics.zip';
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setExportError(failText(t, err));
    } finally {
      setBusy(false);
    }
  };

  const items = log.data?.items ?? [];
  const active = (agents.data ?? []).filter((a) => a.status === 'active');

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t('nav.systemLog')} description={t('systemLog.explain')}>
        <p id={ids.notice} role="note" className="mt-1 text-sm text-warn">
          {t('systemLog.notice')}
        </p>
      </PageHeader>
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <label htmlFor={ids.source} className="text-sm">
            {t('systemLog.source')}
          </label>
          <select
            id={ids.source}
            className={field}
            value={source}
            onChange={(e) => setSource(e.target.value)}
          >
            <option value="panel">{t('systemLog.panel')}</option>
            {active.map((a) => (
              <option key={a.id} value={a.id}>
                {plainText(a.name)}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor={ids.level} className="text-sm">
            {t('systemLog.level')}
          </label>
          <select
            id={ids.level}
            className={field}
            value={level}
            onChange={(e) => setLevel(e.target.value as Level)}
          >
            {levels.map((l) => (
              <option key={l} value={l}>
                {t(`systemLog.levels.${l}`)}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor={ids.period} className="text-sm">
            {t('systemLog.period')}
          </label>
          <select
            id={ids.period}
            className={field}
            value={period}
            onChange={(e) => setPeriod(e.target.value as Period)}
          >
            {(Object.keys(periods) as Period[]).map((p) => (
              <option key={p} value={p}>
                {t(`systemLog.periods.${p}`)}
              </option>
            ))}
          </select>
        </div>
        <Button
          variant="outline"
          className="md:ml-auto"
          disabled={busy}
          aria-describedby={ids.notice}
          onClick={() => void download()}
        >
          {busy ? t('systemLog.exporting') : t('systemLog.export')}
        </Button>
      </div>
      {exportError && (
        <p role="alert" className="text-sm text-danger">
          {exportError}
        </p>
      )}
      {!panel && <p className="text-sm text-muted">{t('systemLog.periodAgent')}</p>}
      <section
        aria-label={t('systemLog.entries')}
        className="rounded-lg border border-border bg-surface"
      >
        {log.isError && (
          <p role="alert" className="border-b border-border px-3 py-2 text-sm text-danger">
            {failText(t, log.error, !panel)}
          </p>
        )}
        {log.data?.truncated && (
          <p className="border-b border-border px-3 py-2 text-sm text-warn">
            {t('systemLog.truncated')}
          </p>
        )}
        {log.isPending && <p className="p-6 text-center text-muted">{t('app.loading')}</p>}
        {log.isSuccess && items.length === 0 && (
          <p className="p-6 text-center text-muted">{t('systemLog.none')}</p>
        )}
        {items.length > 0 && (
          <ol className="h-[60vh] min-h-72 overflow-auto px-3 py-2 font-mono text-xs">
            {items.map((e, i) => (
              <li key={e.id ?? i} className="flex gap-2 py-0.5">
                {e.time && (
                  <time dateTime={e.time} className="shrink-0 text-muted">
                    {dateTime(i18n.language, e.time)}
                  </time>
                )}
                {e.level && (
                  <span
                    className={cn(
                      'shrink-0 uppercase',
                      e.level === 'error' && 'text-danger',
                      e.level === 'warn' && 'text-warn',
                    )}
                  >
                    {t(`systemLog.levelName.${e.level}`)}
                  </span>
                )}
                {e.code && <span className="shrink-0 text-muted">{e.code}</span>}
                <span className="break-all whitespace-pre-wrap">
                  <ForeignText text={e.text} />
                </span>
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}
