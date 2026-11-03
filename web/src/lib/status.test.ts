// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { cardStatus, rconFailing, shownImage } from './status';

const base: Server = {
  id: 's1',
  name: 'mc',
  groups: [],
  status: 'connected',
  status_since: '2026-11-03T10:00:00Z',
};

function withContainer(
  state: string,
  extra: Partial<NonNullable<NonNullable<Server['runtime']>['container']>> = {},
  status: Server['status'] = 'connected',
): Server {
  return {
    ...base,
    status,
    runtime: {
      kind: 'runtime',
      server_id: 's1',
      container: {
        name: 'mc',
        state,
        restart_count: 0,
        restart_loop: false,
        stale: false,
        agent_connected: true,
        ...extra,
      },
    },
  };
}

describe('cardStatus', () => {
  it('uses RCON alone without container data (UI-006)', () => {
    expect(cardStatus(base)).toBe('online');
    expect(cardStatus({ ...base, status: 'offline' })).toBe('offline');
    expect(cardStatus({ ...base, status: 'auth_failed' })).toBe('authFailed');
  });

  it('lets the container lead with agent data (UI-004)', () => {
    expect(cardStatus(withContainer('running'))).toBe('running');
    expect(cardStatus(withContainer('running', {}, 'offline'))).toBe('rconError');
    expect(cardStatus(withContainer('running', { restart_loop: true }))).toBe('restartLoop');
    expect(cardStatus(withContainer('exited', {}, 'offline'))).toBe('stopped');
    expect(cardStatus(withContainer('unknown', { stale: true }, 'offline'))).toBe('unknown');
  });

  it('counts only servers that should answer as RCON failures (UI-003)', () => {
    expect(rconFailing(withContainer('running', {}, 'auth_failed'))).toBe(true);
    expect(rconFailing({ ...base, status: 'offline' })).toBe(true);
    expect(rconFailing(withContainer('exited', {}, 'offline'))).toBe(false);
    expect(rconFailing(withContainer('running'))).toBe(false);
    expect(rconFailing(withContainer('running', { restart_loop: true }, 'offline'))).toBe(true);
    expect(rconFailing(withContainer('unknown', {}, 'offline'))).toBe(false);
  });
});

describe('shownImage', () => {
  it('hides a bare digest (UI-004)', () => {
    expect(shownImage(withContainer('running', { image: 'sha256:abc' }))).toBeUndefined();
    expect(shownImage(withContainer('running', { image: 'itzg/minecraft-server' }))).toBe(
      'itzg/minecraft-server',
    );
    expect(shownImage(base)).toBeUndefined();
  });
});
