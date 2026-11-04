// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import type { Server } from '@/api/servers';
import { ago, decimal, gib, span } from '@/lib/format';
import { stripCodes } from '@/lib/serverpage';
import { cardStatus, shownImage, toneOf, type Tone } from '@/lib/status';
import { cn } from '@/lib/utils';

const edge: Record<Tone, string> = {
  ok: 'border-l-ok',
  warn: 'border-l-warn',
  danger: 'border-l-danger',
  muted: 'border-l-border',
};
const text: Record<Tone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
  muted: 'text-muted',
};
const dot: Record<Tone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  danger: 'bg-danger',
  muted: 'bg-muted',
};

function Meter({
  label,
  value,
  shown,
  tone,
}: {
  label: string;
  value: number;
  shown: string;
  tone: string;
}) {
  const pct = Math.max(0, Math.min(100, value));
  return (
    <div className="min-w-0 flex-1">
      <div className="flex justify-between text-xs">
        <span className="text-muted">{label}</span>
        <span className="font-mono">{shown}</span>
      </div>
      <div
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(pct)}
        className="mt-1 h-1.5 rounded-full bg-surface-raised"
      >
        <div className={cn('h-full rounded-full', tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

// ServerCard shows one server (UI-004): status edge and dot, name, image or
// profile, load, players and last start. Fields without a source are left
// out; stale values are marked, not hidden (Design 6.1). Moderators and
// supporters get no container part from the API, so their card has no
// agent data (UI-006).
export function ServerCard({
  server,
  profile,
  now,
}: {
  server: Server;
  profile?: string;
  now: number;
}) {
  const { t, i18n } = useTranslation();
  const status = cardStatus(server);
  const tone = toneOf[status];
  const rt = server.runtime;
  const c = rt?.container;
  const image = shownImage(server);
  const hasCPU = c?.cpu_percent !== undefined;
  const hasRAM = c?.mem_bytes !== undefined && !!c.mem_limit;
  const lng = i18n.language;
  return (
    <article
      aria-label={server.name}
      data-status={status}
      className={cn(
        'flex min-w-0 flex-col gap-3 rounded-lg border border-l-4 border-border bg-surface p-4',
        edge[tone],
        status === 'stopped' && 'opacity-70',
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="wrap-anywhere md:truncate font-mono font-semibold">
            <Link
              to="/servers/$serverId"
              params={{ serverId: server.id }}
              className="tap max-w-full hover:underline"
              aria-label={t('card.open', { name: server.name })}
            >
              <bdi>{server.name}</bdi>
            </Link>
          </h3>
          {(image ?? profile) && (
            <p className="wrap-anywhere md:truncate font-mono text-sm text-muted">
              {image ? <bdi>{stripCodes(image)}</bdi> : t('card.profile', { name: profile })}
            </p>
          )}
        </div>
        <span className={cn('flex shrink-0 items-center gap-1.5 text-sm', text[tone])}>
          <span className={cn('size-2 rounded-full', dot[tone])} aria-hidden />
          {t(`status.${status}`)}
        </span>
      </div>
      {(hasCPU || hasRAM) && (
        <div className="flex gap-4">
          {hasCPU && (
            <Meter
              label={t('card.cpu')}
              value={c.cpu_percent ?? 0}
              shown={t('card.percent', { value: Math.round(c.cpu_percent ?? 0) })}
              tone={(c.cpu_percent ?? 0) >= 90 ? 'bg-warn' : 'bg-group-4'}
            />
          )}
          {hasRAM && (
            <Meter
              label={t('card.ram')}
              value={((c.mem_bytes ?? 0) / (c.mem_limit ?? 1)) * 100}
              shown={t('card.memory', {
                used: decimal(lng, gib(c.mem_bytes ?? 0)),
                limit: decimal(lng, gib(c.mem_limit ?? 0)),
              })}
              tone="bg-group-1"
            />
          )}
        </div>
      )}
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {rt?.players !== undefined && (
          <span>
            <span className="text-muted">{t('card.players')}</span>{' '}
            <span className="font-mono">{t('card.playersValue', { count: rt.players })}</span>
          </span>
        )}
        {c?.started_at && c.state === 'running' && (
          <span className="text-muted">
            {t('card.started', { ago: ago(t, c.started_at, now) })}
          </span>
        )}
      </div>
      {((c?.stale && c.metrics_at) || rt?.players_stale) && (
        <div className="flex flex-wrap gap-2">
          {c?.stale && c.metrics_at && (
            <span className="rounded border border-warn px-1.5 text-xs text-warn">
              {t('card.stale', { age: span(t, now - Date.parse(c.metrics_at)) })}
            </span>
          )}
          {rt?.players_stale && (
            <span className="rounded border border-warn px-1.5 text-xs text-warn">
              {t('card.playersStale')}
            </span>
          )}
        </div>
      )}
    </article>
  );
}
