// SPDX-License-Identifier: AGPL-3.0-or-later
import { useMutation } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import { useServer } from '@/api/live-servers';
import { useAllowedCommands, useFavorites, useServers } from '@/api/servers';
import { useSession } from '@/api/session';
import { CopyButton } from '@/components/CopyButton';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  addCommands,
  addLogLines,
  MAX_COMMANDS,
  ownCommands,
  suggest,
  updateBuffer,
  useBuffer,
  visible,
  type Block,
  type CmdBlock,
  type Result,
  type Tracks,
} from '@/lib/console';
import { clock } from '@/lib/format';
import { useTopic } from '@/lib/live-context';
import { stripCodes } from '@/lib/serverpage';
import { cn } from '@/lib/utils';

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

// tone colors a block and its marker on the timeline (CMD-013): own
// commands, those of others, logs; errors red.
function tone(b: Block, me?: string): 'own' | 'others' | 'log' | 'error' {
  if (b.kind === 'log') return 'log';
  if (b.result !== 'ok') return 'error';
  return b.userId === me ? 'own' : 'others';
}
const markerClass = {
  own: 'bg-group-4',
  others: 'bg-group-1',
  log: 'bg-muted',
  error: 'bg-danger',
};
const edgeClass = {
  own: 'border-l-group-4',
  others: 'border-l-group-1',
  log: 'border-l-border',
  error: 'border-l-danger',
};

function CommandView({
  b,
  t0,
  collapsed,
  onToggle,
}: {
  b: CmdBlock;
  t0: 'own' | 'others' | 'log' | 'error';
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { t, i18n } = useTranslation();
  const pre = useRef<HTMLPreElement>(null);
  // Answers are foreign data: text only, color codes dropped (SEC-006).
  const answer = b.response ? stripCodes(b.response) : '';
  const copy = answer ? `${b.command}\n${answer}` : b.command;
  return (
    <article
      aria-label={b.command}
      data-tone={t0}
      className={cn('rounded-md border border-l-4 border-border bg-surface', edgeClass[t0])}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
        <button
          type="button"
          className="text-muted"
          aria-expanded={!collapsed}
          aria-label={t(collapsed ? 'console.expand' : 'console.collapse')}
          title={t(collapsed ? 'console.expand' : 'console.collapse')}
          onClick={onToggle}
        >
          {collapsed ? '▸' : '▾'}
        </button>
        <time dateTime={b.ts} className="font-mono text-muted">
          {clock(i18n.language, b.ts)}
        </time>
        {b.pseudonym && <bdi className="font-semibold">{b.pseudonym}</bdi>}
        {b.role && (
          <span className="rounded border border-border px-1.5 text-xs text-muted">
            {t(`role.${b.role}`)}
          </span>
        )}
        <code className="rounded bg-surface-raised px-1.5 py-0.5 font-mono">
          <bdi>{b.command}</bdi>
        </code>
        <span className={cn('ml-auto', b.result === 'ok' ? 'text-ok' : 'text-danger')}>
          {t(`history.result.${b.result}`)}
          {b.reason && <span className="ml-1 font-mono text-xs">({b.reason})</span>}
        </span>
        <CopyButton text={copy} target={pre} label={t('console.copy')} />
      </div>
      {!collapsed && (answer || b.truncated) && (
        <div className="border-t border-border px-3 py-2">
          <pre
            ref={pre}
            className={cn(
              'overflow-x-auto font-mono text-sm whitespace-pre-wrap',
              b.result !== 'ok' && 'text-danger',
            )}
          >
            {answer}
          </pre>
          {b.truncated && <p className="mt-1 text-xs text-warn">{t('history.truncated')}</p>}
        </div>
      )}
    </article>
  );
}

function LogView({ b }: { b: Extract<Block, { kind: 'log' }> }) {
  const { t } = useTranslation();
  return (
    <section
      aria-label={t('console.logLines')}
      data-tone="log"
      className="border-l-2 border-border py-1 pl-3"
    >
      <pre className="overflow-x-auto font-mono text-xs whitespace-pre-wrap text-muted">
        {b.lines.map((l) => stripCodes(l)).join('\n')}
      </pre>
    </section>
  );
}

// FavoritesBar switches between the favorite servers without a reload
// (CMD-001); each keeps its console in memory.
function FavoritesBar({ current }: { current: string }) {
  const { t } = useTranslation();
  const favorites = useFavorites();
  const servers = useServers();
  const list = (favorites.data ?? [])
    .map((id) => servers.data?.find((s) => s.id === id))
    .filter((s) => s !== undefined);
  if (list.length === 0) return null;
  return (
    <nav aria-label={t('console.favorites')} className="flex flex-wrap items-center gap-2">
      <span className="text-sm text-muted">{t('console.favorites')}</span>
      {list.map((s) => (
        <Link
          key={s.id}
          to="/servers/$serverId/console"
          params={{ serverId: s.id }}
          aria-current={s.id === current ? 'page' : undefined}
          className={cn(
            'rounded-md border px-2.5 py-1 font-mono text-sm',
            s.id === current ? 'border-accent text-text' : 'border-border text-muted',
          )}
        >
          <bdi>{s.name}</bdi>
        </Link>
      ))}
    </nav>
  );
}

// ConsoleTab is the admin console (CMD-001, CMD-012 to CMD-015): blocks
// per command, a timeline, tracks with a filter and the input with
// completion and the own commands on the arrow keys.
export function ConsoleTab() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const me = session?.user.id;
  const server = useServer();
  const id = server?.id ?? '';
  const hasAgent = !!server?.agent_id;
  const buf = useBuffer(id);
  const allowed = useAllowedCommands(id, !!id);
  const [tracks, setTracks] = useState<Tracks>({ own: true, others: true, logs: true });
  const [filter, setFilter] = useState('');
  const [input, setInput] = useState('');
  const [walk, setWalk] = useState(-1);
  const [focus, setFocus] = useState(false);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [older, setOlder] = useState(false);
  const scroller = useRef<HTMLDivElement>(null);

  // The last 50 commands come from the history, then live (G1).
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
  // Logs flow only while the track is shown (G2, UI-009).
  useTopic(id && hasAgent && tracks.logs ? `server.${id}.logs` : undefined, (m) => {
    const d = m.data as { lines?: unknown } | undefined;
    if (m.type === 'event' && Array.isArray(d?.lines)) {
      const lines = d.lines.filter((l): l is string => typeof l === 'string');
      updateBuffer(id, (b) => addLogLines(b, lines, new Date().toISOString()));
    }
  });

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
            userId: me,
            pseudonym: session?.user.pseudonym,
            role: session?.user.role,
            command: r.command,
            response: r.response,
            truncated: r.truncated,
            result: 'ok',
          },
        ]),
      ),
    onError: (e, command) => {
      const p = e instanceof ApiError ? e.problem : undefined;
      const status = e instanceof ApiError ? e.status : 0;
      updateBuffer(id, (b) =>
        addCommands(b, [
          {
            kind: 'cmd',
            id: p?.request_id ?? `local-${Date.now()}`,
            ts: new Date().toISOString(),
            userId: me,
            pseudonym: session?.user.pseudonym,
            role: session?.user.role,
            command,
            result: status === 504 ? 'timeout' : status === 403 ? 'denied' : 'error',
            reason: p?.reason ?? undefined,
          },
        ]),
      );
    },
  });

  const items = visible(buf, { ...tracks, logs: tracks.logs && hasAgent }, filter, me);
  // TanStack Virtual keeps its own state, so the React Compiler skips this
  // component; it renders as plain React.
  // eslint-disable-next-line react-hooks/incompatible-library
  const rows = useVirtualizer({
    count: items.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => 72,
    overscan: 8,
    initialRect: { width: 800, height: 600 },
    getItemKey: (i) => items[i]?.id ?? i,
  });
  // Follow new blocks while the view is at the end.
  const atEnd = useRef(true);
  useEffect(() => {
    if (atEnd.current && items.length > 0) rows.scrollToIndex(items.length - 1, { align: 'end' });
  }, [items.length, rows]);

  const paths = (allowed.data?.commands ?? []).map((c) => c.path);
  const suggestions = focus ? suggest(paths, input) : [];
  const own = ownCommands(buf, me);
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Tab' && suggestions[0]) {
      e.preventDefault();
      setInput(`${suggestions[0]} `);
    } else if (e.key === 'ArrowUp' && own.length > 0) {
      e.preventDefault();
      const i = Math.min(walk + 1, own.length - 1);
      setWalk(i);
      setInput(own[i] ?? '');
    } else if (e.key === 'ArrowDown' && walk >= 0) {
      e.preventDefault();
      const i = walk - 1;
      setWalk(i);
      setInput(i >= 0 ? (own[i] ?? '') : '');
    }
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const command = input.trim();
    if (!command || send.isPending) return;
    atEnd.current = true;
    setInput('');
    setWalk(-1);
    send.mutate(command);
  };
  const toggle = (bid: string) =>
    setCollapsed((s) => {
      const n = new Set(s);
      if (n.has(bid)) n.delete(bid);
      else n.add(bid);
      return n;
    });

  if (!server) return null;
  const track = (key: keyof Tracks, label: string, dot: string) => (
    <button
      type="button"
      aria-pressed={tracks[key]}
      onClick={() => setTracks((x) => ({ ...x, [key]: !x[key] }))}
      className={cn(
        'flex min-h-9 items-center gap-1.5 rounded-full border px-3 text-sm',
        tracks[key] ? 'border-accent text-text' : 'border-border text-muted',
      )}
    >
      <span className={cn('size-2 rounded-full', dot)} aria-hidden />
      {label}
    </button>
  );

  return (
    <div className="flex flex-col gap-3">
      <FavoritesBar current={id} />
      <section
        aria-label={t('server.tab.console')}
        className="flex flex-col rounded-lg border border-border bg-surface"
      >
        <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
          <span className="text-sm text-muted">{t('console.tracks')}</span>
          {track('own', t('console.own'), markerClass.own)}
          {track('others', t('console.others'), markerClass.others)}
          {hasAgent && track('logs', t('console.logs'), markerClass.log)}
          <Input
            type="search"
            className="ml-auto w-full sm:w-56"
            placeholder={t('console.filter')}
            aria-label={t('console.filterLabel')}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
        <div className="flex min-h-0">
          {/* The timeline is hidden on a phone (Design 6.1). */}
          <nav
            aria-label={t('console.timeline')}
            className="hidden max-h-[60vh] w-8 shrink-0 flex-col items-center gap-1.5 overflow-y-auto border-r border-border py-3 md:flex"
          >
            {items.map((b, i) => {
              const k = tone(b, me);
              return (
                <button
                  key={b.id}
                  type="button"
                  data-tone={k}
                  aria-label={t('console.jump', {
                    what: b.kind === 'log' ? t('console.logLines') : b.command,
                  })}
                  title={b.kind === 'log' ? t('console.logLines') : b.command}
                  className={cn('size-3 shrink-0 rounded-full', markerClass[k])}
                  onClick={() => {
                    atEnd.current = false;
                    rows.scrollToIndex(i, { align: 'start' });
                  }}
                />
              );
            })}
          </nav>
          <div
            ref={scroller}
            className="h-[60vh] min-h-72 flex-1 overflow-y-auto px-3 py-3"
            onScroll={(e) => {
              const el = e.currentTarget;
              atEnd.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
            }}
          >
            {buf.cursor && buf.cmds.length < MAX_COMMANDS && (
              <Button
                variant="ghost"
                size="sm"
                className="mb-2"
                disabled={older}
                onClick={() => {
                  setOlder(true);
                  atEnd.current = false;
                  void loadPage(buf.cursor)
                    .catch(() => undefined)
                    .finally(() => setOlder(false));
                }}
              >
                {t('console.older')}
              </Button>
            )}
            {buf.loaded && items.length === 0 && (
              <p className="text-sm text-muted">{t('console.empty')}</p>
            )}
            <div className="relative w-full" style={{ height: rows.getTotalSize() }}>
              {rows.getVirtualItems().map((v) => {
                const b = items[v.index];
                if (!b) return null;
                return (
                  <div
                    key={v.key}
                    data-index={v.index}
                    ref={rows.measureElement}
                    className="absolute top-0 left-0 w-full pb-2"
                    style={{ transform: `translateY(${v.start}px)` }}
                  >
                    {b.kind === 'cmd' ? (
                      <CommandView
                        b={b}
                        t0={tone(b, me)}
                        collapsed={collapsed.has(b.id)}
                        onToggle={() => toggle(b.id)}
                      />
                    ) : (
                      <LogView b={b} />
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </div>
        <form onSubmit={submit} className="relative border-t border-border p-3">
          {suggestions.length > 0 && (
            <ul
              aria-label={t('console.suggestions')}
              className="absolute bottom-full left-3 mb-1 w-72 max-w-[calc(100%-1.5rem)] rounded-md border border-border bg-surface-raised py-1"
            >
              {suggestions.map((s, i) => (
                <li key={s}>
                  <button
                    type="button"
                    className={cn(
                      'flex w-full justify-between px-3 py-1.5 text-left font-mono text-sm',
                      i === 0 && 'bg-surface',
                    )}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      setInput(`${s} `);
                    }}
                  >
                    {s}
                    {i === 0 && <span className="text-xs text-muted">{t('console.tab')}</span>}
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="flex gap-2">
            <Input
              className="flex-1 font-mono"
              aria-label={t('console.input')}
              autoComplete="off"
              spellCheck={false}
              value={input}
              onChange={(e) => {
                setInput(e.target.value);
                setWalk(-1);
              }}
              onKeyDown={onKey}
              onFocus={() => setFocus(true)}
              onBlur={() => setFocus(false)}
            />
            <Button type="submit" disabled={!input.trim() || send.isPending}>
              {t('console.send')}
            </Button>
          </div>
          <p className="mt-1 text-xs text-muted">{t('console.hint')}</p>
        </form>
      </section>
    </div>
  );
}
