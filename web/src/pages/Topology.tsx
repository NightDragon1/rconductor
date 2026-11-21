// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { TriangleAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useLiveServers } from '@/api/live-servers';
import { useAgents, type Agent, type Server } from '@/api/servers';
import { useSession } from '@/api/session';
import { ForeignText } from '@/components/ForeignText';
import { byAgent } from '@/lib/hostgroups';
import { cardStatus, toneOf, type Tone } from '@/lib/status';
import { cn } from '@/lib/utils';
import { majorDiffers, majorVersion } from '@/lib/version';
import { PageHeader } from '@/components/PageHeader';

// The status colours of the server cards on the overview (UI-004).
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

// The agents page of the RCON admin; the row of an agent has its ID.
const agentsPage: string = '/admin/agents';

// The default listen addresses of the panel (RCD_HTTP_ADDR,
// RCD_AGENT_ADDR); the session does not carry the configured ones.
const panelPorts = { web: ':8080', agents: ':8443' };

const box =
  'flex flex-col gap-1 rounded-md border border-control bg-surface px-3 py-2 text-sm lg:w-64';

// The lines of the tree from 1024 px on, all at the height of the first
// line of a node (top-5): stub is the line from a node to the trunk of its
// children; branch is a child with its line from the trunk and its part
// of the trunk, which starts at the first and ends at the last branch.
// Below 1024 px the tree is a stacked list without lines.
const stub =
  'lg:relative lg:after:absolute lg:after:top-5 lg:after:left-full lg:after:w-6 lg:after:border-t lg:after:border-control';
const branch = cn(
  'relative lg:pl-6',
  'lg:before:absolute lg:before:top-5 lg:before:left-0 lg:before:w-6 lg:before:border-t lg:before:border-control',
  'lg:after:absolute lg:after:top-0 lg:after:bottom-0 lg:after:left-0 lg:after:border-l lg:after:border-control',
  'lg:first:after:top-5 lg:last:after:bottom-auto lg:last:after:h-[21px] lg:only:after:hidden',
);

// ServerNodes are the servers of a host as small cards like on the
// overview: the status colour on the left edge and in the status.
function ServerNodes({ servers }: { servers: Server[] }) {
  const { t } = useTranslation();
  return (
    <ul className="flex flex-col lg:ml-6">
      {servers.map((s) => {
        const status = cardStatus(s);
        const tone = toneOf[status];
        return (
          <li key={s.id} data-status={status} className={cn(branch, 'pb-2 last:pb-0')}>
            <div
              className={cn(
                'relative flex min-h-10 flex-wrap items-center gap-x-1 rounded-md border border-l-4 border-border bg-surface px-3 text-sm hover:bg-surface-raised has-[a:focus-visible]:bg-surface-raised lg:w-56',
                edge[tone],
              )}
            >
              <Link
                to="/servers/$serverId"
                params={{ serverId: s.id }}
                className="tap wrap-anywhere font-mono after:absolute after:inset-0 hover:underline"
              >
                <bdi>{s.name}</bdi>
              </Link>
              <span className={text[tone]}>
                {t('topology.sep')} {t(`status.${status}`)}
              </span>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

// HostNode is one host with its agent: the display name of the host for
// every role (D-089), below it the agent with its connection state; the
// version, its warning and the link to the agent only for the RCON admin,
// who may read the agents (F10, UI-005). The server admin sees the state
// the runtime data of its servers carry.
function HostNode({
  name,
  agent,
  connected,
  panelVersion,
}: {
  name: ReactNode;
  agent?: Agent;
  connected?: boolean;
  panelVersion: string;
}) {
  const { t } = useTranslation();
  const state = connected === undefined ? 'unknown' : connected ? 'connected' : 'disconnected';
  const tone: Tone = connected === undefined ? 'muted' : connected ? 'ok' : 'danger';
  const differs = agent ? majorDiffers(agent.version, panelVersion) : false;
  const body = (
    <>
      <span className="font-semibold">{name}</span>
      <span className="text-muted">
        {t('topology.agent')}{' '}
        {agent && (
          <>
            <bdi className="font-mono text-text">
              <ForeignText text={agent.version} />
            </bdi>{' '}
          </>
        )}
        {t('topology.sep')} <span className={text[tone]}>{t(`topology.state.${state}`)}</span>
      </span>
      {differs && (
        <span className="flex items-start gap-1 text-warn">
          <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
          {t('topology.majorDiffers', { major: majorVersion(panelVersion) })}
        </span>
      )}
    </>
  );
  if (!agent) return <div className={cn(box, stub)}>{body}</div>;
  return (
    <Link to={agentsPage} hash={agent.id} className={cn(box, stub, 'hover:bg-surface-raised')}>
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
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
          <div className={cn(box, stub, 'shrink-0 lg:w-auto')}>
            <span className="font-semibold">{t('topology.panel')}</span>
            <span className="text-muted">
              {t('topology.panelInfo', { version: panelVersion, ...panelPorts })}
            </span>
          </div>
          <ul aria-label={t('topology.hosts')} className="flex flex-col lg:ml-6">
            {hosts.map((h) => {
              const agent = agents.get(h.agentId);
              const connected =
                agent?.connected ??
                h.servers.find((s) => s.runtime?.container)?.runtime?.container?.agent_connected;
              return (
                <li
                  key={h.agentId}
                  className={cn(branch, 'flex flex-col gap-2 pb-4 last:pb-0 lg:flex-row lg:gap-0')}
                >
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
                    panelVersion={panelVersion}
                  />
                  <ServerNodes servers={h.servers} />
                </li>
              );
            })}
            {rest.length > 0 && (
              <li className={cn(branch, 'flex flex-col gap-2 pb-4 last:pb-0 lg:flex-row lg:gap-0')}>
                <div className={cn(box, stub)}>
                  <span className="font-semibold">{t('topology.noAgent')}</span>
                </div>
                <ServerNodes servers={rest} />
              </li>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
