// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useTranslation } from 'react-i18next';
import type { Agent } from '@/api/servers';
import { ForeignText } from '@/components/ForeignText';

// DOCKER_HOST_FLAG switches the Docker host in place of the agent's own
// address (AGT-014).
export const DOCKER_HOST_FLAG = 'docker_host';

// dockerHostParts are the parts of an agent's Docker host the interface
// shows: name and address as the agent gave them, and the address the agent
// connected from only when there is no host address (AGT-014). The backend
// sends that one only outside the panel's own networks, and all of it to
// the RCON admin only (D-084).
export function dockerHostParts(agent?: Agent) {
  const name = agent?.docker_host?.name;
  const ip = agent?.docker_host?.ip;
  const from = ip ? undefined : agent?.remote;
  return { name, ip, from, any: !!(name || ip || from) };
}

// DockerHost shows the host of an agent's Docker daemon: the name as
// foreign text (SEC-006), the address, or the connection address marked
// as such.
export function DockerHost({ agent }: { agent?: Agent }) {
  const { t } = useTranslation();
  const { name, ip, from, any } = dockerHostParts(agent);
  if (!any) return null;
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-2">
      {(name || ip) && (
        <bdi className="font-mono">
          {name && <ForeignText text={name} />}
          {name && ip && ' '}
          {ip && <span className="text-muted">{name ? `(${ip})` : ip}</span>}
        </bdi>
      )}
      {from && (
        <span className="text-muted">
          {t('agents.connectedFrom')} <bdi className="font-mono">{from}</bdi>
        </span>
      )}
    </span>
  );
}
