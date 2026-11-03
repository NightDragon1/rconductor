// SPDX-License-Identifier: AGPL-3.0-or-later
import { useMutation } from '@tanstack/react-query';
import { useEffect } from 'react';
import { api, ApiError, must } from '@/api/client';
import { useSession } from '@/api/session';
import {
  addCommands,
  addLogLines,
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

// useLogsFeed puts the live log lines of a server into its buffer while
// on: subscribed means followed, so only while someone views the logs
// (UI-009, G2).
export function useLogsFeed(id: string, on: boolean) {
  useTopic(id && on ? `server.${id}.logs` : undefined, (m) => {
    const d = m.data as { lines?: unknown } | undefined;
    if (m.type === 'event' && Array.isArray(d?.lines)) {
      const lines = d.lines.filter((l): l is string => typeof l === 'string');
      updateBuffer(id, (b) => addLogLines(b, lines, new Date().toISOString()));
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

  const loadPage = async (cursor?: string) => {
    const page = must(
      await api.GET('/servers/{id}/history', {
        params: { path: { id }, query: { limit: 50, cursor } },
      }),
    );
    updateBuffer(id, (b) => ({
      ...addCommands(
        b,
        page.items.map((e) => fromWire(e as Wire)),
      ),
      loaded: true,
      cursor: page.next_cursor || undefined,
    }));
  };
  useEffect(() => {
    if (id && !buf.loaded) void loadPage().catch(() => undefined);
    // Only when the server changes; loadPage reads the current id.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, buf.loaded]);

  useTopic(id ? `server.${id}.console` : undefined, (m) => {
    if (m.type === 'event' && m.data && typeof m.data === 'object') {
      updateBuffer(id, (b) => addCommands(b, [fromWire(m.data as Wire)]));
    }
  });
  useLogsFeed(id, logs);

  const own = { userId: me, pseudonym: session?.user.pseudonym, role: session?.user.role };
  const send = useMutation({
    mutationFn: async (command: string) =>
      must(
        await api.POST('/servers/{id}/commands', {
          params: { path: { id } },
          body: { command },
        }),
      ),
    onSuccess: (r) =>
      updateBuffer(id, (b) =>
        addCommands(b, [
          {
            kind: 'cmd',
            id: r.request_id,
            ts: r.ts,
            ...own,
            command: r.command,
            response: r.response,
            truncated: r.truncated,
            result: 'ok',
          },
        ]),
      ),
    onError: (e, command) => {
      const err = e instanceof ApiError ? e : undefined;
      const status = err?.status ?? 0;
      // Refusals of the pipeline are denied, the rest failed (Design 3.5).
      const result: Result =
        status === 504 ? 'timeout' : [403, 422, 429].includes(status) ? 'denied' : 'error';
      updateBuffer(id, (b) =>
        addCommands(b, [
          {
            kind: 'cmd',
            id: err?.problem?.request_id ?? `local-${Date.now()}`,
            ts: new Date().toISOString(),
            ...own,
            command,
            result,
            reason: err?.problem?.reason ?? undefined,
            detail: err?.problem?.detail ?? undefined,
            retryAfter: err?.retryAfter,
          },
        ]),
      );
    },
  });

  const loadOlder = () => (buf.cursor ? loadPage(buf.cursor) : Promise.resolve());
  return { buf, me, send, loadOlder };
}
