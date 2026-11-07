// SPDX-License-Identifier: AGPL-3.0-or-later
import { useSyncExternalStore } from 'react';

export type Result = 'ok' | 'denied' | 'error' | 'timeout';

// CmdBlock is one command with its answer (CMD-012).
export interface CmdBlock {
  kind: 'cmd';
  id: string; // request ID
  ts: string;
  userId?: string;
  pseudonym?: string;
  role?: string;
  command: string;
  response?: string; // foreign data (SEC-006)
  truncated?: boolean;
  result: Result;
  reason?: string;
  detail?: string; // what was wrong, e.g. the parameter (422)
  retryAfter?: number; // seconds, for rate_limited
  local?: boolean; // known only to this tab: the panel refused it before the history
}

// LogBlock holds the log lines of one delivery (CMD-014, UI-009).
export interface LogBlock {
  kind: 'log';
  id: string;
  ts: string;
  lines: string[]; // foreign data (SEC-006)
}

export type Block = CmdBlock | LogBlock;

// Limits per server (G3): commands and log lines are counted apart, so
// logs never push commands out.
export const MAX_COMMANDS = 1000;
export const MAX_LOG_LINES = 500;

export interface ConsoleBuffer {
  cmds: CmdBlock[];
  logs: LogBlock[];
  loaded: boolean; // the first page of the history is in
  cursor?: string; // next older page of the history
  epoch?: number; // counts refills: an older page from before one is void
  running?: number; // commands to this server without an answer yet
  logPos?: { run: string; last: number }; // number of the newest line held
}

// GAP_LINE marks lines that passed while nobody followed.
export const GAP_LINE = '⋯';

// A delivery of log lines; first numbers its first line within run.
export interface LogDelivery {
  lines: string[];
  current?: boolean; // the last lines for a new subscription
  first?: number;
  run?: string;
}

export const emptyBuffer: ConsoleBuffer = { cmds: [], logs: [], loaded: false };

const byTime = (a: { ts: string }, b: { ts: string }) => Date.parse(a.ts) - Date.parse(b.ts);

// addCommands merges commands by request ID: the answer from the REST
// call and the live event of the same command become one block. The
// newest MAX_COMMANDS stay.
export function addCommands(buf: ConsoleBuffer, entries: CmdBlock[]): ConsoleBuffer {
  const map = new Map(buf.cmds.map((c) => [c.id, c]));
  for (const e of entries) {
    const old = map.get(e.id);
    // not_answered is the history of a command still running: it never
    // replaces a result that is here already.
    if (old && e.reason === 'not_answered' && old.reason !== 'not_answered') continue;
    const reason = e.result === 'ok' ? undefined : (e.reason ?? old?.reason);
    map.set(e.id, old ? { ...old, ...e, reason, response: e.response ?? old.response } : e);
  }
  const cmds = [...map.values()].sort(byTime).slice(-MAX_COMMANDS);
  return { ...buf, cmds };
}

// RUNNING_MS is how long the history's not_answered means "still running":
// past the time a command gets, nothing comes any more (#150).
export const RUNNING_MS = 15_000;

export function isRunning(b: CmdBlock, now: number): boolean {
  return b.reason === 'not_answered' && now - Date.parse(b.ts) < RUNNING_MS;
}

let logSeq = 0;

// newLines returns the lines of a delivery that follow what the buffer
// holds: the panel repeats the last lines to every new subscription.
export function newLines(buf: ConsoleBuffer, lines: string[]): string[] {
  const held = buf.logs.flatMap((l) => l.lines);
  for (let n = Math.min(held.length, lines.length); n > 0; n--) {
    if (lines.slice(0, n).every((line, i) => line === held[held.length - n + i])) {
      return lines.slice(n);
    }
  }
  return lines;
}

// addDelivery takes the new lines of a delivery by their numbers; repeated
// lines stay apart that way (#157). Lines that passed meanwhile leave a mark.
// Without a count to go by the text decides.
export function addDelivery(buf: ConsoleBuffer, d: LogDelivery, ts: string): ConsoleBuffer {
  const { lines, first, run } = d;
  if (first === undefined || run === undefined) {
    return addLogLines(buf, d.current ? newLines(buf, lines) : lines, ts);
  }
  const last = first + lines.length - 1;
  const pos = buf.logPos;
  if (pos?.run !== run) {
    const fresh = d.current ? newLines(buf, lines) : lines;
    return { ...addLogLines(buf, fresh, ts), logPos: { run, last } };
  }
  const fresh = lines.slice(Math.max(0, pos.last + 1 - first));
  const gap = first > pos.last + 1;
  return {
    ...addLogLines(buf, gap ? [GAP_LINE, ...fresh] : fresh, ts),
    logPos: { run, last: Math.max(pos.last, last) },
  };
}

// addLogLines appends one delivery of log lines and drops the oldest
// lines over MAX_LOG_LINES.
export function addLogLines(buf: ConsoleBuffer, lines: string[], ts: string): ConsoleBuffer {
  if (lines.length === 0) return buf;
  const logs = [...buf.logs, { kind: 'log' as const, id: `log-${++logSeq}`, ts, lines }];
  let total = logs.reduce((n, l) => n + l.lines.length, 0);
  while (total > MAX_LOG_LINES && logs.length > 0) {
    const first = logs[0];
    if (!first) break;
    const drop = total - MAX_LOG_LINES;
    if (first.lines.length <= drop) {
      logs.shift();
      total -= first.lines.length;
    } else {
      logs[0] = { ...first, lines: first.lines.slice(drop) };
      total -= drop;
    }
  }
  return { ...buf, logs };
}

// prependTail puts earlier lines from logs.tail before the buffer. The tail
// holds what the buffer starts with, and it may go on past the end of the
// buffer (#154), so only what comes before that place goes in; the limit of
// MAX_LOG_LINES then keeps the newest lines (G3).
export function prependTail(buf: ConsoleBuffer, tail: string[]): ConsoleBuffer {
  const have = buf.logs.flatMap((l) => l.lines);
  const fits = (at: number) => {
    const n = Math.min(have.length, tail.length - at);
    return n > 0 && tail.slice(at, at + n).every((line, i) => line === have[i]);
  };
  let start = tail.findIndex((_, at) => fits(at));
  if (start < 0) start = tail.length;
  const room = Math.max(0, MAX_LOG_LINES - have.length);
  const earlier = room === 0 ? [] : tail.slice(0, start).slice(-room);
  if (earlier.length === 0) return buf;
  const first = buf.logs[0];
  const ts = new Date((first ? Date.parse(first.ts) : Date.now()) - 1).toISOString();
  return {
    ...buf,
    logs: [{ kind: 'log', id: `log-${++logSeq}`, ts, lines: earlier }, ...buf.logs],
  };
}

export interface Tracks {
  own: boolean;
  others: boolean;
  logs: boolean;
}

// visible merges the shown tracks in time order and applies the filter
// to command, answer, user and log lines (CMD-014).
export function visible(
  buf: ConsoleBuffer,
  tracks: Tracks,
  filter: string,
  me: string | undefined,
): Block[] {
  const q = filter.trim().toLowerCase();
  const hit = (...parts: (string | undefined)[]) =>
    !q || parts.some((p) => p?.toLowerCase().includes(q));
  const cmds = buf.cmds.filter(
    (c) =>
      (c.userId === me ? tracks.own : tracks.others) && hit(c.command, c.response, c.pseudonym),
  );
  const logs = tracks.logs ? buf.logs.filter((l) => hit(...l.lines)) : [];
  return [...cmds, ...logs].sort(byTime);
}

// suggest completes command names from the catalog (CMD-015): paths that
// start with the input, without arguments.
export function suggest(paths: string[], input: string, max = 8): string[] {
  const q = input.trimStart().toLowerCase();
  if (!q) return [];
  return paths.filter((p) => p.toLowerCase().startsWith(q) && p.toLowerCase() !== q).slice(0, max);
}

// ownCommands lists the caller's commands, newest first, without repeats
// in a row, for the arrow keys (CMD-015).
export function ownCommands(buf: ConsoleBuffer, me: string | undefined): string[] {
  const out: string[] = [];
  for (const c of [...buf.cmds].reverse()) {
    if (c.userId === me && out[out.length - 1] !== c.command) out.push(c.command);
  }
  return out;
}

// The buffers live per server for the life of the tab, so a switch
// between servers keeps both histories (CMD-001).
const buffers = new Map<string, ConsoleBuffer>();
const listeners = new Set<() => void>();

export function updateBuffer(server: string, f: (b: ConsoleBuffer) => ConsoleBuffer) {
  buffers.set(server, f(buffers.get(server) ?? emptyBuffer));
  for (const l of listeners) l();
}

export function useBuffer(server: string): ConsoleBuffer {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => buffers.get(server) ?? emptyBuffer,
  );
}

// resetBuffers is for tests.
export function resetBuffers() {
  buffers.clear();
}

let owner: string | undefined;
let generation = 0;

// bufferOwner names the signed-in user. The buffers hold what this user may
// see, so they go before another user's page renders (Design 6.3).
export function bufferOwner(user: string | undefined) {
  if (user === owner) return;
  owner = user;
  generation += 1;
  buffers.clear();
}

// bufferWriter is updateBuffer for an answer that comes later: taken when a
// request starts, it writes nothing once another user owns the buffers. An
// answer can outlive the session it was asked for.
export function bufferWriter(): typeof updateBuffer {
  const asked = generation;
  return (server, f) => {
    if (asked === generation) updateBuffer(server, f);
  };
}
