// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { Agent, Server, User } from '@/api/servers';
import { clock } from '@/lib/format';
import { cardStatus, isUp, rconFailing } from '@/lib/status';
import { cn } from '@/lib/utils';

function Tile({
  label,
  value,
  sub,
  alert,
}: {
  label: string;
  value: string;
  sub: ReactNode;
  alert?: 'warn' | 'danger';
}) {
  return (
    <section
      aria-label={label}
      className={cn(
        'rounded-lg border bg-surface p-4',
        alert === 'warn' ? 'border-warn' : alert === 'danger' ? 'border-danger' : 'border-border',
      )}
    >
      <h2 className="text-sm text-muted">{label}</h2>
      <p
        className={cn(
          'font-mono text-2xl font-semibold',
          alert === 'warn' && 'text-warn',
          alert === 'danger' && 'text-danger',
        )}
      >
        {value}
      </p>
      <p className="truncate text-sm text-muted">{sub}</p>
    </section>
  );
}

interface Notice {
  key: string;
  tone: 'warn' | 'danger' | 'muted';
  name: string;
  text: string;
  time?: string;
  link: ReactNode;
}

// Pages of later steps; the router builds them from the navigation.
const agentsPage: string = '/admin/agents';
const usersPage: string = '/admin/users';

const dot = { warn: 'bg-warn', danger: 'bg-danger', muted: 'bg-muted' };

// HealthOverview is the head of the RCON admin's landing page (UI-003):
// restart loops, disconnected agents, RCON errors and active and locked
// users, each notice with a link to its object.
export function HealthOverview({
  servers,
  agents,
  users,
}: {
  servers: Server[];
  agents: Agent[];
  users: User[];
}) {
  const { t, i18n } = useTranslation();
  const lng = i18n.language;
  const statuses = servers.map(cardStatus);
  const up = servers.filter(isUp).length;
  const stopped = statuses.filter((s) => s === 'stopped').length;
  const loops = servers.filter((s) => s.runtime?.container?.restart_loop);
  const failing = servers.filter(rconFailing);
  const active = agents.filter((a) => a.status === 'active');
  const gone = active.filter((a) => !a.connected);
  const locked = users.filter((u) => u.disabled);
  const names = (list: { name: string }[]) =>
    list.length ? list.map((s) => s.name).join(', ') : t('health.none');

  const openServer = (s: Server) => (
    <Link to="/servers/$serverId" params={{ serverId: s.id }} className="tap text-accent underline">
      {t('health.openServer')}
    </Link>
  );
  const notices: Notice[] = [
    ...loops.map((s) => ({
      key: `loop-${s.id}`,
      tone: 'warn' as const,
      name: s.name,
      text: t('health.restartLoop'),
      time: s.runtime?.container?.started_at
        ? t('health.at', { time: clock(lng, s.runtime.container.started_at) })
        : undefined,
      link: openServer(s),
    })),
    ...failing.map((s) => ({
      key: `rcon-${s.id}`,
      tone: 'danger' as const,
      name: s.name,
      text:
        (s.status === 'auth_failed' ? t('health.rconAuth') : t('health.rconOffline')) +
        (s.runtime?.container?.state === 'running' ? ` · ${t('health.containerRunning')}` : ''),
      time: t('health.since', { time: clock(lng, s.status_since) }),
      link: openServer(s),
    })),
    ...gone.map((a) => ({
      key: `agent-${a.id}`,
      tone: 'danger' as const,
      name: a.name,
      text: t('health.agentGone'),
      time: a.last_seen_at ? t('health.since', { time: clock(lng, a.last_seen_at) }) : undefined,
      link: (
        <Link to={agentsPage} hash={a.id} className="tap text-accent underline">
          {t('health.openAgents')}
        </Link>
      ),
    })),
    ...locked.map((u) => ({
      key: `user-${u.id}`,
      tone: 'muted' as const,
      name: u.pseudonym,
      text: t('health.userLocked'),
      link: (
        <Link to={usersPage} hash={u.id} className="tap text-accent underline">
          {t('health.openUser')}
        </Link>
      ),
    })),
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5">
        <Tile
          label={t('health.servers')}
          value={`${up} / ${servers.length}`}
          sub={
            t('health.serversSub') +
            (stopped ? ` · ${t('health.stopped', { count: stopped })}` : '')
          }
        />
        <Tile
          label={t('health.agents')}
          value={`${active.length - gone.length} / ${active.length}`}
          sub={t('health.agentsSub')}
          alert={gone.length ? 'danger' : undefined}
        />
        <Tile
          label={t('health.rconErrors')}
          value={String(failing.length)}
          sub={<bdi>{names(failing)}</bdi>}
          alert={failing.length ? 'danger' : undefined}
        />
        <Tile
          label={t('health.restartLoops')}
          value={String(loops.length)}
          sub={<bdi>{names(loops)}</bdi>}
          alert={loops.length ? 'warn' : undefined}
        />
        <Tile
          label={t('health.users')}
          value={String(users.length - locked.length)}
          sub={t('health.usersSub', { count: locked.length })}
        />
      </div>
      <section
        aria-label={t('health.notices')}
        className="rounded-lg border border-border bg-surface"
      >
        <h2 className="px-4 pt-3 pb-2 text-sm font-semibold">{t('health.notices')}</h2>
        {notices.length === 0 ? (
          <p className="px-4 pb-3 text-sm text-muted">{t('health.noNotices')}</p>
        ) : (
          <ul>
            {notices.map((n) => (
              <li
                key={n.key}
                className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-t border-border px-4 py-2.5 text-sm"
              >
                {/* On a phone the time and the link wrap below the text. */}
                <div className="flex min-w-0 flex-1 basis-64 items-baseline gap-x-3">
                  <span
                    className={cn('size-2 shrink-0 self-center rounded-full', dot[n.tone])}
                    aria-hidden
                  />
                  <bdi className="min-w-0 truncate font-mono font-semibold">{n.name}</bdi>
                  <span className="min-w-0 text-muted">{n.text}</span>
                </div>
                <div className="ml-5 flex items-baseline gap-x-3 sm:ml-auto">
                  {n.time && <span className="text-muted">{n.time}</span>}
                  {n.link}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
