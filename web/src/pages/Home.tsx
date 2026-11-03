// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  applyOverview,
  useAgents,
  useProfiles,
  useServerGroups,
  useServers,
  useUsers,
  type Server,
  type ServerGroup,
} from '@/api/servers';
import { useSession } from '@/api/session';
import { HealthOverview } from '@/components/HealthOverview';
import { ServerCard } from '@/components/ServerCard';
import { Input } from '@/components/ui/input';
import { useNow } from '@/lib/format';
import { useTopic } from '@/lib/live-context';
import { isUp } from '@/lib/status';
import { cn } from '@/lib/utils';

const frames = [
  'border-group-1 text-group-1',
  'border-group-2 text-group-2',
  'border-group-3 text-group-3',
  'border-group-4 text-group-4',
  'border-group-5 text-group-5',
  'border-group-6 text-group-6',
];
const dots = ['bg-group-1', 'bg-group-2', 'bg-group-3', 'bg-group-4', 'bg-group-5', 'bg-group-6'];

function Cards({
  servers,
  profiles,
  now,
}: {
  servers: Server[];
  profiles: Map<string, string>;
  now: number;
}) {
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
      {servers.map((s) => (
        <ServerCard
          key={s.id}
          server={s}
          profile={s.profile_id ? profiles.get(s.profile_id) : undefined}
          now={now}
        />
      ))}
    </div>
  );
}

function GroupFrame({
  name,
  index,
  servers,
  children,
}: {
  name: string;
  index: number;
  servers: Server[];
  children: React.ReactNode;
}) {
  const { t } = useTranslation();
  const ok = servers.filter(isUp).length;
  const frame =
    (index < 0 ? undefined : frames[index % frames.length]) ?? 'border-border text-muted';
  return (
    <section
      aria-label={name}
      className={cn('rounded-xl border bg-surface/40 p-4', frame.split(' ')[0])}
    >
      <div className="mb-3 flex items-center gap-3">
        <h2
          className={cn(
            'flex items-center gap-2 rounded-md border px-2.5 py-1 font-mono text-sm font-semibold uppercase',
            frame,
          )}
        >
          <span
            className={cn(
              'size-2 rounded-full',
              index < 0 ? 'bg-muted' : dots[index % dots.length],
            )}
            aria-hidden
          />
          <bdi>{name}</bdi>
        </h2>
        <span className="text-sm text-muted">
          {t('home.groupSummary', { count: servers.length, ok })}
        </span>
      </div>
      {children}
    </section>
  );
}

// byGroups sorts the cards into their server groups (Gesamtkonzept 5);
// a server in two groups shows in both, servers without a group last.
function byGroups(servers: Server[], groups: ServerGroup[]) {
  const ids = new Set(servers.map((s) => s.id));
  const out = [...groups]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((g) => ({ group: g, servers: servers.filter((s) => g.server_ids.includes(s.id)) }))
    .filter((g) => g.servers.length > 0);
  const grouped = new Set(out.flatMap((g) => g.servers.map((s) => s.id)));
  const rest = servers.filter((s) => ids.has(s.id) && !grouped.has(s.id));
  return { groups: out, rest };
}

// HomePage is the landing page of every role (UI-002, Gesamtkonzept 5):
// the RCON admin gets the health overview and all cards by server group,
// the server admin the cards of his scope by group, moderator and
// supporter reduced cards of their servers. The cards stay live over
// servers.overview.
export function HomePage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: session } = useSession();
  const role = session?.user.role;
  const admin = role === 'rcon_admin' || role === 'server_admin';
  const servers = useServers();
  const groups = useServerGroups(admin);
  const profiles = useProfiles();
  const agents = useAgents(role === 'rcon_admin');
  const users = useUsers(role === 'rcon_admin');
  const now = useNow();
  const [query, setQuery] = useState('');
  useTopic(session ? 'servers.overview' : undefined, (m) => {
    if (m.type === 'event') applyOverview(qc, m.data);
  });

  const all = servers.data ?? [];
  const q = query.trim().toLowerCase();
  const shown = q ? all.filter((s) => s.name.toLowerCase().includes(q)) : all;
  const profileNames = new Map((profiles.data ?? []).map((p) => [p.id, p.name]));
  const summary = [
    t('home.summaryServers', { count: all.length }),
    admin && groups.data ? t('home.summaryGroups', { count: groups.data.length }) : undefined,
    role === 'rcon_admin' && agents.data
      ? t('home.summaryAgents', { count: agents.data.filter((a) => a.status === 'active').length })
      : undefined,
  ].filter(Boolean);

  let cards: React.ReactNode;
  if (servers.isPending) {
    cards = <p className="text-muted">{t('app.loading')}</p>;
  } else if (servers.isError) {
    cards = <p className="text-danger">{t('app.error')}</p>;
  } else if (all.length === 0) {
    cards = <p className="text-muted">{t('home.empty')}</p>;
  } else if (shown.length === 0) {
    cards = <p className="text-muted">{t('home.noMatch')}</p>;
  } else if (admin && groups.data && byGroups(shown, groups.data).groups.length > 0) {
    const { groups: sections, rest } = byGroups(shown, groups.data);
    cards = (
      <>
        {sections.map((g, i) => (
          <GroupFrame key={g.group.id} name={g.group.name} index={i} servers={g.servers}>
            <Cards servers={g.servers} profiles={profileNames} now={now} />
          </GroupFrame>
        ))}
        {rest.length > 0 && (
          <GroupFrame name={t('home.ungrouped')} index={-1} servers={rest}>
            <Cards servers={rest} profiles={profileNames} now={now} />
          </GroupFrame>
        )}
      </>
    );
  } else {
    cards = <Cards servers={shown} profiles={profileNames} now={now} />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">{t('home.title')}</h1>
          <p className="text-sm text-muted">{summary.join(' · ')}</p>
        </div>
        <Input
          type="search"
          className="w-full sm:w-64"
          placeholder={t('home.search')}
          aria-label={t('home.searchLabel')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {role === 'rcon_admin' && servers.data && agents.data && users.data && (
        <HealthOverview servers={servers.data} agents={agents.data} users={users.data} />
      )}
      {cards}
    </div>
  );
}
