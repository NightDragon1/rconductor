// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Server } from '@/api/servers';

export type CardStatus =
  | 'running'
  | 'healthy'
  | 'starting'
  | 'unhealthy'
  | 'paused'
  | 'restarting'
  | 'dead'
  | 'restartLoop'
  | 'rconError'
  | 'stopped'
  | 'unknown'
  | 'online'
  | 'offline'
  | 'authFailed'
  | 'certMismatch';

export type Tone = 'ok' | 'warn' | 'danger' | 'muted';

export const toneOf: Record<CardStatus, Tone> = {
  running: 'ok',
  healthy: 'ok',
  starting: 'warn',
  unhealthy: 'danger',
  paused: 'warn',
  restarting: 'warn',
  dead: 'danger',
  online: 'ok',
  restartLoop: 'warn',
  rconError: 'danger',
  offline: 'danger',
  authFailed: 'danger',
  certMismatch: 'danger',
  stopped: 'muted',
  unknown: 'muted',
};

// summaryOf puts a card status into the running, stopped or error part of
// a summary (UI-021): a healthy or still starting container runs, a paused
// or restarting one is stopped for now, unhealthy and dead are errors.
export function summaryOf(st: CardStatus): 'running' | 'stopped' | 'error' {
  if (st === 'running' || st === 'online' || st === 'healthy' || st === 'starting')
    return 'running';
  if (st === 'stopped' || st === 'paused' || st === 'restarting') return 'stopped';
  return 'error';
}

// isUp: the server runs and answers over RCON.
export const isUp = (s: Server) => toneOf[cardStatus(s)] === 'ok';

// cardStatus is the status of a server card (UI-004). With container data
// (admins, server with agent) the container state leads and RCON tells a
// running container's health; without it the status comes from the RCON
// reachability alone (UI-006). A running container with a healthcheck
// shows its result instead of running (UI-021, UI-011); paused, restarting
// and dead keep their own name, every other state is stopped.
export function cardStatus(s: Server): CardStatus {
  const c = s.runtime?.container;
  if (!c) {
    if (s.status === 'connected') return 'online';
    if (s.status === 'cert_mismatch') return 'certMismatch';
    return s.status === 'auth_failed' ? 'authFailed' : 'offline';
  }
  if (c.state === 'unknown') return 'unknown';
  if (c.state === 'paused' || c.state === 'restarting' || c.state === 'dead') return c.state;
  if (c.state !== 'running') return 'stopped';
  if (c.restart_loop) return 'restartLoop';
  if (s.status !== 'connected') return 'rconError';
  return c.health ?? 'running';
}

// rconFailing reports an RCON problem that the overview lists (UI-003): a
// server that should answer but does not, also in a restart loop. Without
// an agent, or while its agent is gone (state unknown), nothing tells that
// the server is stopped, so RCON offline is an error. Only a container
// known to be stopped is not one.
export function rconFailing(s: Server): boolean {
  if (s.status === 'connected') return false;
  const c = s.runtime?.container;
  return !c || c.state === 'running' || c.state === 'unknown';
}

// shownImage is the image of a card; a bare digest says nothing, so the
// card shows the game profile instead (UI-004).
export function shownImage(s: Server): string | undefined {
  const img = s.runtime?.container?.image;
  if (!img || img.startsWith('sha256:')) return undefined;
  return img;
}
