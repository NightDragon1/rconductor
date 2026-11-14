// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { useLiveServers } from '@/api/live-servers';
import { useAgents, useServerGroups, type Agent, type Server } from '@/api/servers';
import { useSession } from '@/api/session';
import { byGroups } from '@/lib/groups';
import { cardStatus, toneOf, type Tone } from '@/lib/status';
import { cn } from '@/lib/utils';
import { ForeignText } from '@/components/ForeignText';

const dot: Record<Tone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  danger: 'bg-danger',
  muted: 'bg-muted',
};

// AgentNode is one host with its agent. The name only for the RCON admin,
// who may read the agents (F10); the server admin sees "Agent" and the
// state the runtime data of its servers carry (UI-010).
function AgentNode({
  agent,
  connected,
  servers,
}: {
  agent?: Agent;
  connected?: boolean;
  servers: Server[];
}) {
  const { t } = useTranslation();
  const state = connected === undefined ? 'unknown' : connected ? 'connected' : 'disconnected';
  const tone: Tone = connected === undefined ? 'muted' : connected ? 'ok' : 'danger';
  return (
    <li className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <span className="text-muted">{t('topology.host')}</span>
        {agent?.remote && <bdi className="font-mono">{agent.remote}</bdi>}
        <span aria-hidden>→</span>
        <span className="flex items-center gap-1.5 rounded-md border border-border px-2 py-0.5">
          <span className={cn('size-2 rounded-full', dot[tone])} aria-hidden />
          {agent ? (
            <bdi className="font-mono">
              <ForeignText text={agent.name} />
            </bdi>
          ) : (
            t('topology.agent')
          )}
          <span className="text-muted">· {t(`topology.state.${state}`)}</span>
        </span>
      </div>
      <ServerNodes servers={servers} />
    </li>
  );
}

function ServerNodes({ servers }: { servers: Server[] }) {
  const { t } = useTranslation();
  return (
    <ul className="ml-4 flex flex-col gap-1 border-l border-border pl-4">
      {servers.map((s) => {
        const status = cardStatus(s);
        return (
          <li key={s.id} className="flex items-center gap-2 text-sm">
            <span className={cn('size-2 rounded-full', dot[toneOf[status]])} aria-hidden />
            <Link
              to="/servers/$serverId"
              params={{ serverId: s.id }}
              className="tap font-mono underline"
            >
              <bdi>{s.name}</bdi>
            </Link>
            <span className="text-muted">{t(`status.${status}`)}</span>
          </li>
        );
      })}
    </ul>
  );
}

// Tree lays out one group: the agents with their servers, then the
// servers without agent.
function Tree({ servers, agents }: { servers: Server[]; agents: Map<string, Agent> }) {
  const { t } = useTranslation();
  const byAgent = new Map<string, Server[]>();
  const none: Server[] = [];
  for (const s of servers) {
    if (!s.agent_id) none.push(s);
    else byAgent.set(s.agent_id, [...(byAgent.get(s.agent_id) ?? []), s]);
  }
  return (
    <ul className="flex flex-col gap-4">
      {[...byAgent.entries()].map(([id, list]) => (
        <AgentNode
          key={id}
          agent={agents.get(id)}
          connected={list.find((s) => s.runtime?.container)?.runtime?.container?.agent_connected}
          servers={list}
        />
      ))}
      {none.length > 0 && (
        <li className="flex flex-col gap-2">
          <span className="text-sm text-muted">{t('topology.noAgent')}</span>
          <ServerNodes servers={none} />
        </li>
      )}
    </ul>
  );
}

// TopologyPage is the live graph host → agent → game server, by server
// group, for RCON admin and server admin in their scope (UI-010). It
// follows servers.overview like the cards; the backend decides what the
// role gets.
export function TopologyPage() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const rcon = session?.user.role === 'rcon_admin';
  const servers = useLiveServers();
  const groups = useServerGroups(true);
  const agentList = useAgents(rcon);
  // Agent names only for the RCON admin (F10), even if a cache held them.
  const agents = new Map(rcon ? (agentList.data ?? []).map((a) => [a.id, a]) : []);
  const all = servers.data ?? [];
  const { groups: sections, rest } = byGroups(all, groups.data ?? []);
  return (
    <div className="flex flex-col gap-5">
      <h1 className="text-2xl font-semibold">{t('nav.topology')}</h1>
      {servers.isPending && <p className="text-muted">{t('app.loading')}</p>}
      {!servers.isPending && all.length === 0 && <p className="text-muted">{t('home.empty')}</p>}
      {sections.map((g) => (
        <section
          key={g.group.id}
          aria-label={g.group.name}
          className="rounded-lg border border-border bg-surface p-4"
        >
          <h2 className="mb-3 text-sm font-semibold uppercase">
            <bdi>{g.group.name}</bdi>
          </h2>
          <Tree servers={g.servers} agents={agents} />
        </section>
      ))}
      {rest.length > 0 && (
        <section
          aria-label={t('home.ungrouped')}
          className="rounded-lg border border-border bg-surface p-4"
        >
          <h2 className="mb-3 text-sm font-semibold">{t('home.ungrouped')}</h2>
          <Tree servers={rest} agents={agents} />
        </section>
      )}
    </div>
  );
}
