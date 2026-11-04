// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link, Outlet } from '@tanstack/react-router';
import { ChevronLeft, Star } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  useAgents,
  useAllowedCommands,
  useFavorites,
  useSetFavorites,
  useProfiles,
  useServerGroups,
  type Server,
} from '@/api/servers';
import { useSession } from '@/api/session';
import { useServer } from '@/api/live-servers';
import { decimal, gib, span, useNow } from '@/lib/format';
import { usePhone } from '@/lib/phone';
import { isAdmin, tabsFor } from '@/lib/serverpage';
import { cardStatus, toneOf, type Tone } from '@/lib/status';
import { cn } from '@/lib/utils';
import { ServerActions } from './ServerActions';

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

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <span>
      <span className="text-muted">{label}</span> <span className="font-mono">{children}</span>
    </span>
  );
}

// FavoriteStar puts the server on the admin's favorites for the console
// (CMD-001).
function FavoriteStar({ id }: { id: string }) {
  const { t } = useTranslation();
  const favorites = useFavorites();
  const set = useSetFavorites();
  const on = favorites.data?.includes(id) ?? false;
  const label = t(on ? 'server.unfavorite' : 'server.favorite');
  return (
    <button
      type="button"
      aria-pressed={on}
      aria-label={label}
      title={label}
      disabled={!favorites.data || set.isPending}
      className="flex size-9 items-center justify-center rounded-md text-muted hover:text-text max-md:size-11"
      onClick={() => {
        const ids = favorites.data ?? [];
        set.mutate(on ? ids.filter((x) => x !== id) : [...ids, id]);
      }}
    >
      <Star aria-hidden className={cn('size-5', on && 'fill-warn text-warn')} />
    </button>
  );
}

function Head({ server, role }: { server: Server; role?: string }) {
  const { t, i18n } = useTranslation();
  const now = useNow();
  const phone = usePhone();
  const admin = isAdmin(role);
  const profiles = useProfiles();
  const agents = useAgents(role === 'rcon_admin' && !!server.agent_id);
  const commands = useAllowedCommands(server.id, !admin);
  const status = cardStatus(server);
  const tone = toneOf[status];
  const c = server.runtime?.container;
  const profile = profiles.data?.find((p) => p.id === server.profile_id)?.name;
  // Only the RCON admin may read the agents; the server admin sees the
  // state alone (F10).
  const agentName = agents.data?.find((a) => a.id === server.agent_id)?.name;
  const lng = i18n.language;
  return (
    <section
      aria-label={server.name}
      className={cn(
        'flex flex-wrap items-start justify-between gap-4 rounded-lg border border-l-4 border-border bg-surface p-5 max-md:gap-2 max-md:p-3',
        edge[tone],
      )}
    >
      {phone && (
        <Link
          to="/servers"
          aria-label={t('nav.servers')}
          title={t('nav.servers')}
          className="flex size-11 shrink-0 items-center justify-center rounded-md border border-border"
        >
          <ChevronLeft aria-hidden className="size-5" />
        </Link>
      )}
      <div className="flex min-w-0 flex-col gap-2 max-md:flex-1">
        <div className="flex flex-wrap items-center gap-3 max-md:gap-x-2 max-md:gap-y-0">
          <h1 className="max-w-full truncate font-mono text-2xl font-semibold max-md:text-lg">
            <bdi>{server.name}</bdi>
          </h1>
          {admin && <FavoriteStar id={server.id} />}
          <span className={cn('flex items-center gap-1.5 text-sm', text[tone])}>
            <span className={cn('size-2 rounded-full', dot[tone])} aria-hidden />
            {status === 'running' && c?.started_at
              ? t('server.runningSince', { span: span(t, now - Date.parse(c.started_at)) })
              : t(`status.${status}`)}
          </span>
        </div>
        <div className="flex flex-wrap gap-x-5 gap-y-1 text-sm max-md:gap-x-3">
          {profile && (
            <span className="max-md:hidden">
              <span className="text-muted">{t('server.profile')}</span> {profile}
            </span>
          )}
          {server.runtime?.players !== undefined && (
            <Fact label={t('server.players')}>
              {t('card.playersValue', { count: server.runtime.players })}
            </Fact>
          )}
          {c?.cpu_percent !== undefined && (
            <Fact label={t('card.cpu')}>
              {t('card.percent', { value: Math.round(c.cpu_percent) })}
            </Fact>
          )}
          {c?.mem_bytes !== undefined && !!c.mem_limit && (
            <Fact label={t('card.ram')}>
              {t('card.memory', {
                used: decimal(lng, gib(c.mem_bytes)),
                limit: decimal(lng, gib(c.mem_limit)),
              })}
            </Fact>
          )}
          {admin && server.agent_id && c && (
            <span className="max-md:hidden">
              <span className="text-muted">{t('server.agent')}</span>{' '}
              {agentName && <bdi className="mr-1">{agentName}</bdi>}
              <span className={c.agent_connected ? 'text-ok' : 'text-danger'}>
                {t(c.agent_connected ? 'server.agentConnected' : 'server.agentGone')}
              </span>
            </span>
          )}
          {!admin && commands.data && (
            <span>
              <span className="text-muted">{t('server.commandList')}</span>{' '}
              {t('server.commandCount', { count: commands.data.commands.length })}
            </span>
          )}
        </div>
      </div>
      <ServerActions server={server} role={role} />
    </section>
  );
}

// ServerLayout is the server page (Design 6.1, 6.4, UI-011): breadcrumb,
// head with status and actions, and the tabs of the role (Design 6.3).
export function ServerLayout() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const role = session?.user.role;
  const server = useServer();
  const groups = useServerGroups(isAdmin(role));
  if (!server) return <p className="text-muted">{t('app.loading')}</p>;
  const group = groups.data?.find((g) => server.groups.includes(g.id));
  const tabs = tabsFor(role, !!server.agent_id);
  return (
    <div className="flex flex-col gap-5">
      <nav aria-label={t('server.breadcrumb')} className="text-sm text-muted max-md:hidden">
        <Link to="/servers" className="underline">
          {t('nav.servers')}
        </Link>
        {group && (
          <>
            {' / '}
            <bdi className="uppercase">{group.name}</bdi>
          </>
        )}
        {' / '}
        <bdi className="text-text">{server.name}</bdi>
      </nav>
      <Head server={server} role={role} />
      <nav
        aria-label={t('server.tabs')}
        className="flex gap-1 overflow-x-auto border-b border-border"
      >
        {tabs.map((tab) => (
          <Link
            key={tab}
            to={`/servers/$serverId/${tab}`}
            params={{ serverId: server.id }}
            className="-mb-px border-b-2 border-transparent px-4 py-2.5 whitespace-nowrap text-muted hover:text-text"
            activeProps={{
              className: 'border-accent! text-text! font-semibold',
              'aria-current': 'page',
            }}
          >
            {t(`server.tab.${tab}`)}
          </Link>
        ))}
      </nav>
      <Outlet />
    </div>
  );
}
