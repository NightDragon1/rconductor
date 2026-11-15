// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useTranslation } from 'react-i18next';
import type { Agent } from '@/api/servers';
import { ForeignText } from '@/components/ForeignText';

// DOCKER_HOST_FLAG switches the Docker host in place of the agent's own
// address (AGT-014).
export const DOCKER_HOST_FLAG = 'docker_host';

// DockerHost shows the host of an agent's Docker daemon: the name as
// foreign text (SEC-006) and the address, each only if the daemon gave it.
// Without an address from the daemon it names the address the agent
// connected from, marked as such (D-084). The backend sends both to the
// RCON admin only.
export function DockerHost({ agent }: { agent?: Agent }) {
  const { t } = useTranslation();
  const name = agent?.docker_host?.name;
  const ip = agent?.docker_host?.ip;
  const from = ip ? undefined : agent?.remote;
  if (!name && !ip && !from) return null;
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
