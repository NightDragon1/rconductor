// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { TriangleAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useLiveServers } from '@/api/live-servers';
import { useAgents, type Agent, type Server } from '@/api/servers';
import { useSession } from '@/api/session';
import { DOCKER_HOST_FLAG, DockerHost } from '@/components/DockerHost';
import { ForeignText } from '@/components/ForeignText';
import { byAgent } from '@/lib/hostgroups';
import { cardStatus, toneOf, type Tone } from '@/lib/status';
import { cn } from '@/lib/utils';
import { majorDiffers, majorVersion } from '@/lib/version';
import { PageHeader } from '@/components/PageHeader';

const dot: Record<Tone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  danger: 'bg-danger',
  muted: 'bg-muted',
};

// The agents page of the RCON admin; the row of an agent has its ID.
const agentsPage: string = '/admin/agents';

const box =
  'flex flex-col gap-1 rounded-md border border-control bg-surface px-3 py-2 text-sm md:w-64';

// Branch is one child of a column with the line to it: on wide screens a
// stub left of the child, on phones the indented line of the list only.
function Branch({ children }: { children: ReactNode }) {
  return (
    <li className="relative flex flex-col gap-3 md:flex-row md:items-start md:gap-6 md:before:absolute md:before:top-5 md:before:-left-6 md:before:w-6 md:before:border-t md:before:border-control">
      {children}
    </li>
  );
}

function ServerNodes({ servers }: { servers: Server[] }) {
  const { t } = useTranslation();
  return (
    <ul className="ml-4 flex flex-col gap-1 border-l border-control pl-4 md:ml-0 md:pl-6">
      {servers.map((s) => {
        const status = cardStatus(s);
        return (
          <li
            key={s.id}
            className="relative flex items-center gap-2 text-sm md:before:absolute md:before:top-1/2 md:before:-left-6 md:before:w-5 md:before:border-t md:before:border-control"
          >
            <span className={cn('size-2 shrink-0 rounded-full', dot[toneOf[status]])} aria-hidden />
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

// HostNode is one host with its agent. The display name of the host for
// every role (D-089); agent name, address, version and the link to the
// agent only for the RCON admin, who may read the agents (F10, UI-005).
// The server admin sees the state the runtime data of its servers carry.
function HostNode({
  name,
  agent,
  connected,
  dockerHost,
  panelVersion,
}: {
  name: ReactNode;
  agent?: Agent;
  connected?: boolean;
  dockerHost: boolean;
  panelVersion: string;
}) {
  const { t } = useTranslation();
  const state = connected === undefined ? 'unknown' : connected ? 'connected' : 'disconnected';
  const tone: Tone = connected === undefined ? 'muted' : connected ? 'ok' : 'danger';
  const differs = agent ? majorDiffers(agent.version, panelVersion) : false;
  const body = (
    <>
      <span className="font-semibold">{name}</span>
      {agent && (
        <span className="text-muted">
          {t('topology.agent')}{' '}
          <bdi className="font-mono text-text">
            <ForeignText text={agent.name} />
          </bdi>
        </span>
      )}
      {/* With the flag docker_host the host of the daemon replaces the
          agent's own, often internal, address (AGT-014). */}
      {agent &&
        (dockerHost ? (
          <DockerHost agent={agent} />
        ) : (
          agent.remote && <bdi className="font-mono text-muted">{agent.remote}</bdi>
        ))}
      {agent && (
        <span className="text-muted">
          {t('topology.version')}{' '}
          <bdi className="font-mono text-text">
            <ForeignText text={agent.version} />
          </bdi>
        </span>
      )}
      <span className="flex items-center gap-1.5">
        <span className={cn('size-2 rounded-full', dot[tone])} aria-hidden />
        <span className="text-muted">{t(`topology.state.${state}`)}</span>
      </span>
      {differs && (
        <span className="flex items-start gap-1 text-warn">
          <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
          {t('topology.majorDiffers', { major: majorVersion(panelVersion) })}
        </span>
      )}
    </>
  );
  if (!agent) return <div className={box}>{body}</div>;
  return (
    <Link to={agentsPage} hash={agent.id} className={cn(box, 'hover:bg-surface-raised')}>
      {body}
    </Link>
  );
}

// TopologyPage is the live tree panel → host with its agent → game server,
// from left to right, for RCON admin and server admin in their scope
// (UI-010). It follows servers.overview like the cards; the backend
// decides what the role gets.
export function TopologyPage() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const rcon = session?.user.role === 'rcon_admin';
  const dockerHost = (session?.ui.features ?? []).includes(DOCKER_HOST_FLAG);
  const panelVersion = session?.panel_version ?? '';
  const servers = useLiveServers();
  const agentList = useAgents(rcon);
  // Agent data only for the RCON admin (F10), even if a cache held them.
  const agents = new Map(rcon ? (agentList.data ?? []).map((a) => [a.id, a]) : []);
  const all = servers.data ?? [];
  const { hosts, rest } = byAgent(
    all,
    new Map([...agents.values()].map((a) => [a.id, a.display_name])),
  );
  let unnamed = 0;
  return (
    <div className="flex flex-col gap-5">
      <PageHeader title={t('nav.topology')} description={t('topology.explain')} />
      {servers.isPending && <p className="text-muted">{t('app.loading')}</p>}
      {!servers.isPending && all.length === 0 && <p className="text-muted">{t('home.empty')}</p>}
      {all.length > 0 && (
        <div className="flex flex-col gap-3 md:flex-row md:items-start md:gap-6">
          <div className={cn(box, 'shrink-0 md:w-auto')}>
            <span className="font-semibold">{t('topology.panel')}</span>
            <span className="text-muted">
              {t('topology.version')} <bdi className="font-mono text-text">{panelVersion}</bdi>
            </span>
          </div>
          <ul
            aria-label={t('topology.hosts')}
            className="ml-4 flex flex-col gap-4 border-l border-control pl-4 md:ml-0 md:pl-6"
          >
            {hosts.map((h) => {
              const agent = agents.get(h.agentId);
              const connected =
                agent?.connected ??
                h.servers.find((s) => s.runtime?.container)?.runtime?.container?.agent_connected;
              return (
                <Branch key={h.agentId}>
                  <HostNode
                    name={
                      h.displayName ? (
                        <ForeignText text={h.displayName} />
                      ) : (
                        t('home.hostUnnamed', { n: ++unnamed })
                      )
                    }
                    agent={agent}
                    connected={connected}
                    dockerHost={dockerHost}
                    panelVersion={panelVersion}
                  />
                  <ServerNodes servers={h.servers} />
                </Branch>
              );
            })}
            {rest.length > 0 && (
              <Branch>
                <div className={box}>
                  <span className="font-semibold">{t('topology.noAgent')}</span>
                </div>
                <ServerNodes servers={rest} />
              </Branch>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
