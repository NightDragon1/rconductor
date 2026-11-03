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
    map.set(e.id, old ? { ...old, ...e, response: e.response ?? old.response } : e);
  }
  const cmds = [...map.values()].sort(byTime).slice(-MAX_COMMANDS);
  return { ...buf, cmds };
}

let logSeq = 0;

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

// prependTail puts earlier lines from logs.tail before the buffer. The
// tail ends with lines the buffer may already hold, so only the part
// before the longest overlap goes in; the limit of MAX_LOG_LINES then
// keeps the newest lines (G3).
export function prependTail(buf: ConsoleBuffer, tail: string[]): ConsoleBuffer {
  const have = buf.logs.flatMap((l) => l.lines);
  let overlap = 0;
  for (let k = Math.min(tail.length, have.length); k > 0; k--) {
    if (tail.slice(tail.length - k).every((line, i) => line === have[i])) {
      overlap = k;
      break;
    }
  }
  const room = Math.max(0, MAX_LOG_LINES - have.length);
  const earlier = tail.slice(0, tail.length - overlap).slice(-room);
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
