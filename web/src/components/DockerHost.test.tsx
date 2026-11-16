// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, it } from 'vitest';
import { dockerHostParts } from '@/components/DockerHost';
import type { Agent } from '@/api/servers';

const agent = (a: Partial<Agent>) => ({ id: 'a', name: 'n', status: 'active', ...a }) as Agent;

describe('Docker host parts (AGT-014, D-084)', () => {
  it('shows nothing for an agent without host and address, so no label stands empty', () => {
    expect(dockerHostParts(agent({})).any).toBe(false);
    expect(dockerHostParts(undefined).any).toBe(false);
  });

  it('shows the connection address only without a host address', () => {
    expect(dockerHostParts(agent({ remote: '203.0.113.7' })).from).toBe('203.0.113.7');
    const both = dockerHostParts(
      agent({ remote: '203.0.113.7', docker_host: { name: 'nas', ip: '10.0.0.5' } }),
    );
    expect(both.from).toBeUndefined();
    expect(both.ip).toBe('10.0.0.5');
    expect(dockerHostParts(agent({ docker_host: { name: 'nas' } })).any).toBe(true);
  });
});
