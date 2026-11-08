// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Server } from '@/api/servers';

export type Role = 'rcon_admin' | 'server_admin' | 'moderator' | 'supporter';
export type Tab = 'overview' | 'console' | 'commands' | 'logs' | 'history';

export const isAdmin = (role: string | undefined) =>
  role === 'rcon_admin' || role === 'server_admin';

// agentCan tells whether the server's agent reported a capability
// (AGT-013). While the panel has not seen the agent since its start the
// capabilities are unknown and count as present; the backend refuses an
// order without the capability anyway.
export function agentCan(s: Server, capability: string): boolean {
  const caps = s.runtime?.container?.capabilities;
  return caps === undefined || caps.includes(capability);
}

// tabsFor lists the tabs of the server page a role sees (Design 6.3,
// UI-011). Logs need an agent that reports the capability logs; without
// one they have no source (F11, AGT-013).
export function tabsFor(role: string | undefined, hasAgent: boolean, logs = true): Tab[] {
  if (isAdmin(role)) {
    return hasAgent && logs
      ? ['overview', 'console', 'logs', 'history']
      : ['overview', 'console', 'history'];
  }
  if (role === 'moderator' || role === 'supporter') return ['overview', 'commands', 'history'];
  return [];
}

// defaultTab is where the server page opens: the console for admins, the
// commands for moderator and supporter (F12).
export function defaultTab(role: string | undefined): Tab {
  return isAdmin(role) ? 'console' : 'commands';
}

export type ActionMode = 'none' | 'agent' | 'rcon';

// actionMode: actions only for RCON admin and server admin (AGT-008); with
// an agent start, stop and restart, without one only the restart over the
// profile's stop command (AGT-009, revision 2.4 candidate 16). An agent
// without the capability container.action gets no actions (AGT-013).
export function actionMode(s: Server, role: string | undefined): ActionMode {
  if (!isAdmin(role)) return 'none';
  if (!s.agent_id) return 'rcon';
  return agentCan(s, 'container.action') ? 'agent' : 'none';
}

export interface ActionState {
  start: boolean;
  stop: boolean;
  restart: boolean;
  agentGone: boolean;
}

// actionState tells which buttons work in the current state.
export function actionState(s: Server, mode: ActionMode): ActionState {
  if (mode === 'rcon') {
    return { start: false, stop: false, restart: s.status === 'connected', agentGone: false };
  }
  const c = s.runtime?.container;
  if (mode === 'none' || !c || !c.agent_connected || c.state === 'unknown') {
    return { start: false, stop: false, restart: false, agentGone: mode === 'agent' };
  }
  const running = c.state === 'running' || c.state === 'restarting';
  return { start: !running, stop: running, restart: running, agentGone: false };
}

// stripCodes removes Minecraft color and format codes (§x) from foreign
// text: in 1.0 they are dropped, never turned into styles (revision 2.4
// candidate 9, SEC-006).
export function stripCodes(text: string): string {
  return text.replace(/§[0-9a-fk-orx]/gi, '');
}
