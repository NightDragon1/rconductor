// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { byGroups } from '@/lib/groups';
import { byHost, hostCounts } from '@/lib/hostgroups';
import { ForeignText } from '@/components/ForeignText';
import { useProfiles, useServerGroups, type Server } from '@/api/servers';
import { useSession } from '@/api/session';
import { useLiveServers } from '@/api/live-servers';
import { GroupFrame } from '@/components/GroupFrame';
import { PageHeader } from '@/components/PageHeader';
import { ServerCard } from '@/components/ServerCard';
import { Input } from '@/components/ui/input';
import { useNow } from '@/lib/format';
import { topProfile } from '@/lib/profiles';
import { isUp } from '@/lib/status';

function Cards({
  servers,
  profiles,
  now,
}: {
  servers: Server[];
  profiles: { id: string; name: string }[];
  now: number;
}) {
  const { t } = useTranslation();
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
      {servers.map((s) => (
        <ServerCard
          key={s.id}
          server={s}
          profile={topProfile(t, s.profile_ids, profiles) || undefined}
          now={now}
        />
      ))}
    </div>
  );
}

// ServerBoard is a page of server cards with a search: the overview and
// the server list (F13). Admins see the cards by server group (Gesamtkonzept
// 5); with hosts, every role sees them by host instead (UI-021). Summary and
// head come from the page.
export function ServerBoard({
  title,
  description,
  summary = [],
  head,
  hosts = false,
}: {
  title: string;
  description: string;
  summary?: (string | undefined)[];
  head?: ReactNode;
  hosts?: boolean;
}) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const role = session?.user.role;
  const admin = role === 'rcon_admin' || role === 'server_admin';
  const servers = useLiveServers();
  const groups = useServerGroups(admin);
  const profiles = useProfiles();
  const now = useNow();
  const [query, setQuery] = useState('');
  const groupSummary = (list: Server[]) =>
    t('home.groupSummary', { count: list.length, ok: list.filter(isUp).length });

  const all = servers.data ?? [];
  const q = query.trim().toLowerCase();
  const shown = q ? all.filter((s) => s.name.toLowerCase().includes(q)) : all;
  const profileNames = profiles.data ?? [];
  const line = [
    t('home.summaryServers', { count: all.length }),
    admin && groups.data ? t('home.summaryGroups', { count: groups.data.length }) : undefined,
    ...summary,
  ].filter(Boolean);

  let cards: ReactNode;
  if (servers.isPending) {
    cards = <p className="text-muted">{t('app.loading')}</p>;
  } else if (servers.isError) {
    cards = <p className="text-danger">{t('app.error')}</p>;
  } else if (all.length === 0) {
    cards = <p className="text-muted">{t('home.empty')}</p>;
  } else if (shown.length === 0) {
    cards = <p className="text-muted">{t('home.noMatch')}</p>;
  } else if (hosts) {
    // Each host in a frame with its display name or a neutral placeholder,
    // never the host name (D-089); not collapsible, nothing remembered.
    const { hosts: list, rest } = byHost(shown);
    let unnamed = 0;
    cards = (
      <>
        {list.map((g) => {
          const name = g.displayName ?? t('home.hostUnnamed', { n: ++unnamed });
          return (
            <GroupFrame
              key={g.key}
              name={name}
              title={g.displayName ? <ForeignText text={g.displayName} /> : name}
              summary={t('home.hostSummary', { count: g.servers.length, ...g.counts })}
            >
              <Cards servers={g.servers} profiles={profileNames} now={now} />
            </GroupFrame>
          );
        })}
        {rest.length > 0 && (
          <GroupFrame
            name={t('home.withoutAgent')}
            summary={t('home.hostSummary', { count: rest.length, ...hostCounts(rest) })}
          >
            <Cards servers={rest} profiles={profileNames} now={now} />
          </GroupFrame>
        )}
      </>
    );
  } else if (admin && groups.data && byGroups(shown, groups.data).groups.length > 0) {
    const { groups: sections, rest } = byGroups(shown, groups.data);
    cards = (
      <>
        {sections.map((g) => (
          <GroupFrame key={g.group.id} name={g.group.name} summary={groupSummary(g.servers)}>
            <Cards servers={g.servers} profiles={profileNames} now={now} />
          </GroupFrame>
        ))}
        {rest.length > 0 && (
          <GroupFrame name={t('home.ungrouped')} summary={groupSummary(rest)}>
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
        <PageHeader title={title} description={description}>
          <p className="text-sm text-muted">{line.join(' · ')}</p>
        </PageHeader>
        <Input
          type="search"
          className="w-full sm:w-64"
          placeholder={t('home.search')}
          aria-label={t('home.searchLabel')}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      {head}
      {cards}
    </div>
  );
}
