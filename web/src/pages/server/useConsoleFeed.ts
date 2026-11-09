// SPDX-License-Identifier: AGPL-3.0-or-later
import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { api, ApiError, must } from '@/api/client';
import { useSession } from '@/api/session';
import {
  addCommands,
  addDelivery,
  bufferWriter,
  updateBuffer,
  useBuffer,
  type CmdBlock,
  type Result,
} from '@/lib/console';
import { useTopic } from '@/lib/live-context';

interface Wire {
  request_id: string;
  ts: string;
  user_id?: string;
  pseudonym?: string;
  role?: string;
  command: string;
  response?: string;
  truncated?: boolean;
  result: Result;
  reason?: string;
}

const fromWire = (e: Wire): CmdBlock => ({
  kind: 'cmd',
  id: e.request_id,
  ts: e.ts,
  userId: e.user_id,
  pseudonym: e.pseudonym,
  role: e.role,
  command: e.command,
  response: e.response,
  truncated: e.truncated,
  result: e.result,
  reason: e.reason,
});

// latestRefill numbers the refills of each server.
const latestRefill = new Map<string, number>();

// useLogsFeed puts the live log lines of a server into its buffer while
// on: subscribed means followed, so only while someone views the logs
// (UI-009, G2).
export function useLogsFeed(id: string, on: boolean) {
  const [write] = useState(bufferWriter);
  useTopic(id && on ? `server.${id}.logs` : undefined, (m) => {
    const d = m.data as
      { lines?: unknown; current?: unknown; first?: unknown; run?: unknown } | undefined;
    if (m.type === 'event' && Array.isArray(d?.lines)) {
      const lines = d.lines.filter((l): l is string => typeof l === 'string');
      const ts = new Date().toISOString();
      const first = typeof d.first === 'number' ? d.first : undefined;
      const run = typeof d.run === 'string' ? d.run : undefined;
      write(id, (b) => addDelivery(b, { lines, current: d.current === true, first, run }, ts));
    }
  });
}

// useConsoleFeed feeds the command buffer of a server for the console
// and the command page: the last 50 commands from the history (G1), then
// live over server.<id>.console, the logs while wanted (G2), and sending
// with the answer merged into the live event by request ID.
export function useConsoleFeed(id: string, logs: boolean) {
  const { data: session } = useSession();
  const me = session?.user.id;
  const buf = useBuffer(id);
  const [confirmed, setConfirmed] = useState(0);
  // Taken at mount: a socket can still deliver between a change of user and
  // its end, and that must not reach the buffers of the next user.
  const [live] = useState(bufferWriter);

  const loadPage = async (cursor?: string) => {
    const epoch = buf.epoch;
    const write = bufferWriter();
    const refill = cursor ? 0 : (latestRefill.get(id) ?? 0) + 1;
    if (refill) latestRefill.set(id, refill);
    const page = must(
      await api.GET('/servers/{id}/history', {
        params: { path: { id }, query: { limit: 50, cursor } },
      }),
    );
    const items = page.items.map((e) => fromWire(e as Wire));
    if (cursor) {
      write(id, (b) =>
        b.epoch === epoch ? { ...addCommands(b, items), cursor: page.next_cursor || undefined } : b,
      );
      return null;
    }
    // Only the refill asked for last counts: an earlier one knows less.
    if (latestRefill.get(id) !== refill) return null;
    // A refill starts over from the newest page, so older pages stay
    // reachable without a gap, whatever arrived meanwhile. Kept are live
    // commands newer than the page and refusals only this tab knows.
    const ids = new Set(items.map((e) => e.id));
    const newest = Math.max(-Infinity, ...items.map((e) => Date.parse(e.ts)));
    write(id, (b) => ({
      ...addCommands(
        {
          ...b,
          cmds: b.cmds.filter((c) => ids.has(c.id) || c.local || Date.parse(c.ts) > newest),
        },
        items,
      ),
      loaded: true,
      cursor: page.next_cursor || undefined,
      epoch: (b.epoch ?? 0) + 1,
    }));
    return null;
  };
  // A query, so that it also runs after a reconnect of the socket (Design
  // 2.3) and retries: no live event reached this buffer meanwhile. Each
  // confirmed subscription fetches again: only from then on live events come,
  // so nothing can fall between the page and the first event.
  const history = useQuery({
    queryKey: ['console-history', id, confirmed],
    queryFn: () => loadPage(),
    enabled: !!id,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });

  useTopic(id ? `server.${id}.console` : undefined, (m) => {
    if (m.type === 'subscribed') setConfirmed((n) => n + 1);
    if (m.type === 'event' && m.data && typeof m.data === 'object') {
      live(id, (b) => addCommands(b, [fromWire(m.data as Wire)]));
    }
  });
  useLogsFeed(id, logs);

  // In the buffer, so a tab that remounts still knows what is under way.
  const count = (write: typeof updateBuffer, server: string, by: number) =>
    write(server, (b) => ({ ...b, running: Math.max(0, (b.running ?? 0) + by) }));
  const own = { userId: me, pseudonym: session?.user.pseudonym, role: session?.user.role };
  // The server travels with the command: the answer belongs to the server
  // it was sent to, also when the user has switched meanwhile (CMD-001),
  // and to the user who sent it (Design 6.3).
  const mutation = useMutation({
    mutationFn: async ({
      server,
      command,
    }: {
      server: string;
      command: string;
      write: typeof updateBuffer;
    }) =>
      must(
        await api.POST('/servers/{id}/commands', {
          params: { path: { id: server } },
          body: { command },
        }),
      ),
    onMutate: ({ server, write }) => count(write, server, 1),
    onSettled: (_r, _e, { server, write }) => count(write, server, -1),
    onSuccess: (r, { server, write }) =>
      write(server, (b) =>
        addCommands(b, [
          {
            kind: 'cmd',
            id: r.request_id,
            ts: r.ts,
            ...own,
            command: r.command,
            response: r.response,
            truncated: r.truncated,
            // error with reason command_failed: the game reported it as
            // failed; its text stays in response (D-073).
            result: r.result,
            reason: r.reason ?? undefined,
          },
        ]),
      ),
    onError: (e, { server, command, write }) => {
      const err = e instanceof ApiError ? e : undefined;
      const status = err?.status ?? 0;
      // Refusals of the pipeline are denied, the rest failed (Design 3.5).
      const result: Result =
        status === 504 ? 'timeout' : [403, 422, 429].includes(status) ? 'denied' : 'error';
      write(server, (b) =>
        addCommands(b, [
          {
            kind: 'cmd',
            id: err?.problem?.request_id ?? `local-${Date.now()}`,
            ts: new Date().toISOString(),
            ...own,
            command,
            result,
            local: true,
            reason: err?.problem?.reason ?? undefined,
            detail: err?.problem?.detail ?? undefined,
            retryAfter: err?.retryAfter,
          },
        ]),
      );
    },
  });

  const send = {
    mutate: (command: string) => mutation.mutate({ server: id, command, write: bufferWriter() }),
    // Only a command to this server blocks its input.
    isPending: (buf.running ?? 0) > 0,
  };
  const loadOlder = () => (buf.cursor ? loadPage(buf.cursor) : Promise.resolve());
  return { buf, me, send, loadOlder, history };
}
