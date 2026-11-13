// SPDX-License-Identifier: AGPL-3.0-or-later
import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { playerCount } from '@/lib/players';
import { usePlayers } from '@/api/servers';
import { useSession } from '@/api/session';
import { ago, dateTime, decimal, gib, useNow } from '@/lib/format';
import { useTopic } from '@/lib/live-context';
import { isAdmin, stripCodes } from '@/lib/serverpage';
import { cardStatus } from '@/lib/status';
import { useServer } from '@/api/live-servers';
import { PortChips } from '@/components/PortChips';
import { portsOf } from '@/lib/ports';
import { ForeignText } from '@/components/ForeignText';

interface Metrics {
  cpu_percent: number;
  mem_bytes: number;
  mem_limit: number;
  net_rx: number;
  net_tx: number;
  blk_read: number;
  blk_write: number;
  at: string;
  stale: boolean;
}

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="rounded-lg border border-border bg-surface p-4">
      <h2 className="mb-3 text-sm font-semibold">{title}</h2>
      {children}
    </section>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-1 text-sm">
      <dt className="text-muted">{label}</dt>
      <dd className="font-mono">{children}</dd>
    </div>
  );
}

// OverviewTab shows status, the players online and, for admins of a
// server with agent, the load (Design 6.3); for admins also the ports and
// the volumes with their size and its age (UI-005). Values that are old
// are marked (Design 6.1).
export function OverviewTab() {
  const { t, i18n } = useTranslation();
  const lng = i18n.language;
  const { data: session } = useSession();
  const server = useServer();
  const now = useNow();
  const players = usePlayers(server?.id ?? '');
  const withLoad = isAdmin(session?.user.role) && !!server?.agent_id;
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  useTopic(withLoad && server ? `server.${server.id}.metrics` : undefined, (m) => {
    if (m.type === 'event' && m.data && typeof m.data === 'object') setMetrics(m.data as Metrics);
  });
  if (!server) return null;

  const mb = (bytes: number) =>
    bytes >= 2 ** 30
      ? t('overview.bytes', { value: decimal(lng, gib(bytes)), unit: 'GB' })
      : t('overview.bytes', { value: decimal(lng, bytes / 2 ** 20), unit: 'MB' });
  const p = players.data;
  const pc = playerCount(p);
  const admin = isAdmin(session?.user.role);
  const ports = admin ? portsOf(server) : [];
  const volumes = admin ? (server.runtime?.container?.volumes ?? []) : [];
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Panel title={t('overview.status')}>
        <dl>
          <Row label={t('overview.status')}>{t(`status.${cardStatus(server)}`)}</Row>
          <Row label={t('overview.rcon')}>
            {t('overview.since', { time: dateTime(lng, server.status_since) })}
          </Row>
        </dl>
      </Panel>
      <Panel title={t('overview.players')}>
        {p && !p.available && <p className="text-sm text-muted">{t('overview.noPlayerList')}</p>}
        {p?.available && (
          <p className="text-sm" data-testid="player-count">
            {pc.max
              ? t('overview.playerCountOf', { count: pc.count, max: pc.max })
              : t('overview.playerCount', { count: pc.count })}
          </p>
        )}
        {p?.available && pc.count === 0 && (
          <p className="text-sm text-muted">{t('overview.noPlayers')}</p>
        )}
        {pc.noNames && <p className="text-sm text-muted">{t('overview.noNames')}</p>}
        {p?.available && p.players.length > 0 && (
          <ul className="flex flex-wrap gap-2">
            {p.players.map(stripCodes).map((name) => (
              <li
                key={name}
                className="rounded-md border border-border px-2 py-1 font-mono text-sm"
              >
                <bdi>{name}</bdi>
              </li>
            ))}
          </ul>
        )}
        {p?.updated_at && (
          <p className={p.stale ? 'mt-3 text-xs text-warn' : 'mt-3 text-xs text-muted'}>
            {t('overview.playersAt', { ago: ago(t, p.updated_at, now) })}
            {p.stale && ` · ${t('overview.stale')}`}
          </p>
        )}
      </Panel>
      {ports.length > 0 && (
        <Panel title={t('ports.label')}>
          <PortChips ports={ports} label={t('ports.label')} />
        </Panel>
      )}
      {volumes.length > 0 && (
        <Panel title={t('volumes.label')}>
          <ul className="flex flex-col gap-1">
            {volumes.map((v) => (
              <li key={v.name} className="flex flex-wrap justify-between gap-x-4 text-sm">
                <bdi className="font-mono">
                  <ForeignText text={v.name} />
                </bdi>
                {v.size_bytes !== undefined && v.measured_at && (
                  <span className={v.stale ? 'text-warn' : 'text-muted'}>
                    {t('volumes.size', { size: mb(v.size_bytes), ago: ago(t, v.measured_at, now) })}
                    {v.stale && ` · ${t('overview.stale')}`}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </Panel>
      )}
      {withLoad && (
        <Panel title={t('overview.load')}>
          {metrics ? (
            <>
              <dl>
                <Row label={t('overview.cpu')}>
                  {t('card.percent', { value: Math.round(metrics.cpu_percent) })}
                </Row>
                <Row label={t('overview.ram')}>
                  {t('card.memory', {
                    used: decimal(lng, gib(metrics.mem_bytes)),
                    limit: decimal(lng, gib(metrics.mem_limit)),
                  })}
                </Row>
                <Row label={t('overview.net')}>
                  {t('overview.netValue', { rx: mb(metrics.net_rx), tx: mb(metrics.net_tx) })}
                </Row>
                <Row label={t('overview.disk')}>
                  {t('overview.diskValue', {
                    read: mb(metrics.blk_read),
                    write: mb(metrics.blk_write),
                  })}
                </Row>
              </dl>
              <p className={metrics.stale ? 'mt-3 text-xs text-warn' : 'mt-3 text-xs text-muted'}>
                {t('overview.metricsAt', { ago: ago(t, metrics.at, now) })}
                {metrics.stale && ` · ${t('overview.stale')}`}
              </p>
            </>
          ) : (
            <p className="text-sm text-muted">{t('overview.noMetrics')}</p>
          )}
        </Panel>
      )}
    </div>
  );
}
