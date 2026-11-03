// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { actionMode, actionState, defaultTab, stripCodes, tabsFor } from './serverpage';

const base: Server = {
  id: 's1',
  name: 'mc',
  groups: [],
  status: 'connected',
  status_since: '',
  version: 'v',
};

function agentServer(state: string, connected = true): Server {
  return {
    ...base,
    agent_id: 'a1',
    runtime: {
      kind: 'runtime',
      server_id: 's1',
      container: {
        name: 'mc',
        state,
        restart_count: 0,
        restart_loop: false,
        stale: false,
        agent_connected: connected,
      },
    },
  };
}

describe('tabs (Design 6.3, UI-011)', () => {
  it('gives admins console and logs, logs only with an agent', () => {
    expect(tabsFor('rcon_admin', true)).toEqual(['overview', 'console', 'logs', 'history']);
    expect(tabsFor('server_admin', false)).toEqual(['overview', 'console', 'history']);
  });
  it('gives moderator and supporter the commands', () => {
    expect(tabsFor('moderator', true)).toEqual(['overview', 'commands', 'history']);
    expect(tabsFor('supporter', false)).toEqual(['overview', 'commands', 'history']);
    expect(tabsFor(undefined, true)).toEqual([]);
  });
  it('opens the main work of the role (F12)', () => {
    expect(defaultTab('rcon_admin')).toBe('console');
    expect(defaultTab('server_admin')).toBe('console');
    expect(defaultTab('moderator')).toBe('commands');
    expect(defaultTab('supporter')).toBe('commands');
  });
});

describe('actions (AGT-008, AGT-009)', () => {
  it('offers none to moderator and supporter', () => {
    expect(actionMode(agentServer('running'), 'moderator')).toBe('none');
    expect(actionMode(agentServer('running'), 'supporter')).toBe('none');
  });
  it('uses the agent when there is one, else RCON', () => {
    expect(actionMode(agentServer('running'), 'server_admin')).toBe('agent');
    expect(actionMode(base, 'rcon_admin')).toBe('rcon');
  });
  it('follows the container state', () => {
    expect(actionState(agentServer('running'), 'agent')).toEqual({
      start: false,
      stop: true,
      restart: true,
      agentGone: false,
    });
    expect(actionState(agentServer('exited'), 'agent')).toEqual({
      start: true,
      stop: false,
      restart: false,
      agentGone: false,
    });
    expect(actionState(agentServer('unknown', false), 'agent')).toEqual({
      start: false,
      stop: false,
      restart: false,
      agentGone: true,
    });
  });
  it('restarts over RCON only while RCON answers', () => {
    expect(actionState(base, 'rcon').restart).toBe(true);
    expect(actionState({ ...base, status: 'offline' }, 'rcon').restart).toBe(false);
    expect(actionState(base, 'rcon').stop).toBe(false);
  });
});

describe('stripCodes (SEC-006)', () => {
  it('drops Minecraft codes and keeps the text', () => {
    expect(stripCodes('§ahello §lworld§r!')).toBe('hello world!');
    expect(stripCodes('§§a')).toBe('§');
    expect(stripCodes('<b>x</b>')).toBe('<b>x</b>');
  });
});
