// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import { useAgents, useServers, useUsers } from '@/api/servers';
import { useSession } from '@/api/session';
import { HealthOverview } from '@/components/HealthOverview';
import { ServerBoard } from '@/components/ServerBoard';

// HomePage is the landing page of every role (UI-002, Gesamtkonzept 5):
// the RCON admin gets the health overview and all cards by server group,
// the server admin the cards of his scope by group, moderator and
// supporter reduced cards of their servers. The cards stay live over
// servers.overview.
export function HomePage() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const rconAdmin = session?.user.role === 'rcon_admin';
  const servers = useServers();
  const agents = useAgents(rconAdmin);
  const users = useUsers(rconAdmin);
  return (
    <ServerBoard
      title={t('home.title')}
      summary={[
        rconAdmin && agents.data
          ? t('home.summaryAgents', {
              count: agents.data.filter((a) => a.status === 'active').length,
            })
          : undefined,
      ]}
      head={
        rconAdmin &&
        servers.data &&
        agents.data &&
        users.data && (
          <HealthOverview servers={servers.data} agents={agents.data} users={users.data} />
        )
      }
    />
  );
}
