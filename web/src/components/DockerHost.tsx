// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { Agent } from '@/api/servers';
import { ForeignText } from '@/components/ForeignText';

// DOCKER_HOST_FLAG switches the Docker host in place of the agent's own
// address (AGT-014).
export const DOCKER_HOST_FLAG = 'docker_host';

// DockerHost shows the host of an agent's Docker daemon: the name as
// foreign text (SEC-006) and the address, each only if the daemon gave it.
// The backend sends it to the RCON admin only.
export function DockerHost({ host }: { host?: Agent['docker_host'] }) {
  if (!host?.name && !host?.ip) return null;
  return (
    <bdi className="font-mono">
      {host.name && <ForeignText text={host.name} />}
      {host.name && host.ip && ' '}
      {host.ip && <span className="text-muted">{host.name ? `(${host.ip})` : host.ip}</span>}
    </bdi>
  );
}
