// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useServer } from '@/api/live-servers';
import { useAllowedCommands, useFavorites, useServers } from '@/api/servers';
import { useSession } from '@/api/session';
import { CommandPalette } from '@/components/CommandPalette';
import { CopyButton } from '@/components/CopyButton';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  isRunning,
  MAX_COMMANDS,
  missingSlash,
  ownCommands,
  suggest,
  visible,
  type Block,
  type CmdBlock,
  type Tracks,
} from '@/lib/console';
import { playerRule } from '@/lib/commandform';
import { clock } from '@/lib/format';
import { stripCodes } from '@/lib/serverpage';
import { cn } from '@/lib/utils';
import { MultiCommand } from './MultiCommand';
import { TemplatesPanel } from './TemplatesPanel';
import { useConsoleFeed } from './useConsoleFeed';

// tone colors a block and its marker on the timeline (CMD-013): own
// commands, those of others, logs; errors red.
function tone(b: Block, me: string | undefined, now: number): 'own' | 'others' | 'log' | 'error' {
  if (b.kind === 'log') return 'log';
  if (b.result !== 'ok' && !isRunning(b, now)) return 'error';
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
  now,
  collapsed,
  onToggle,
}: {
  b: CmdBlock;
  now: number;
  t0: 'own' | 'others' | 'log' | 'error';
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { t, i18n } = useTranslation();
  const pre = useRef<HTMLPreElement>(null);
  // Answers are foreign data: text only, color codes dropped (SEC-006).
  // The detail of a refusal comes from the panel, not the game server.
  const answer = b.response ? stripCodes(b.response) : (b.detail ?? '');
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
          className="tap justify-center text-muted"
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
          <ForeignText text={b.command} />
        </code>
        {isRunning(b, now) ? (
          <span className="ml-auto text-muted">{t('console.running')}</span>
        ) : (
          <span className={cn('ml-auto', b.result === 'ok' ? 'text-ok' : 'text-danger')}>
            {t(`history.result.${b.result}`)}
            {b.reason && <span className="ml-1 font-mono text-xs">({b.reason})</span>}
          </span>
        )}
        <CopyButton text={copy} target={pre} label={t('console.copy')} />
      </div>
      {!collapsed && (answer || b.truncated) && (
        <div className="border-t border-border px-3 py-2">
          <pre
            ref={pre}
            className={cn(
              'overflow-x-auto font-mono text-sm wrap-anywhere whitespace-pre-wrap',
              b.result !== 'ok' && 'text-danger',
            )}
          >
            <ForeignText text={answer} />
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
      <pre className="overflow-x-auto font-mono text-xs wrap-anywhere whitespace-pre-wrap text-muted">
        <ForeignText text={b.lines.map((l) => stripCodes(l)).join('\n')} />
      </pre>
    </section>
  );
}

// SlashHint warns before sending when every command of the server starts
// with "/" and the input does not (CAT-005); it never changes the input.
export function SlashHint({ paths, input }: { paths: string[]; input: string }) {
  const { t } = useTranslation();
  if (!missingSlash(paths, input)) return null;
  return (
    <p role="status" className="mt-1 text-xs text-warn">
      {t('console.slashHint')}
    </p>
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
            'tap rounded-md border px-2.5 py-1 font-mono text-sm',
            s.id === current ? 'border-accent text-text' : 'border-control text-muted',
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
  const allowed = useAllowedCommands(id, !!id);
  // Running commands turn into failed ones without any event.
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const i = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(i);
  }, []);
  const [allTracks, setAllTracks] = useState<Record<string, Tracks>>({});
  const tracks = allTracks[id] ?? { own: true, others: true, logs: true };
  const [filters, setFilters] = useState<Record<string, string>>({});
  const filter = filters[id] ?? '';
  const setFilter = (text: string) => setFilters((f) => ({ ...f, [id]: text }));
  // A draft belongs to its server: a switch must not carry it to another.
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const input = drafts[id] ?? '';
  const setInput = (text: string) => setDrafts((d) => ({ ...d, [id]: text }));
  const [walks, setWalks] = useState<Record<string, number>>({});
  const walk = walks[id] ?? -1;
  const setWalk = (i: number) => setWalks((w) => ({ ...w, [id]: i }));
  const [focus, setFocus] = useState(false);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [older, setOlder] = useState(false);
  const [templatesOpen, setTemplatesOpen] = useState(false);
  const [multiOpen, setMultiOpen] = useState(false);
  const scroller = useRef<HTMLDivElement>(null);
  // Logs flow only while their track is shown (G2, UI-009).
  const { buf, send, loadOlder, history } = useConsoleFeed(id, hasAgent && tracks.logs);

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
      onClick={() => setAllTracks((a) => ({ ...a, [id]: { ...tracks, [key]: !tracks[key] } }))}
      className={cn(
        'flex min-h-9 shrink-0 items-center gap-1.5 rounded-full border px-3 text-sm max-md:min-h-11',
        tracks[key] ? 'border-accent text-text' : 'border-control text-muted',
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
          <div className="flex max-w-full flex-wrap items-center gap-2">
            <span className="text-sm text-muted max-md:hidden">{t('console.tracks')}</span>
            {track('own', t('console.own'), markerClass.own)}
            {track('others', t('console.others'), markerClass.others)}
            {hasAgent && track('logs', t('console.logs'), markerClass.log)}
          </div>
          <div className="ml-auto flex flex-wrap gap-2">
            <CommandPalette
              serverId={id}
              onUse={(command) => {
                setInput(command);
                setWalk(-1);
              }}
            />
            <Button variant="outline" size="sm" onClick={() => setTemplatesOpen(true)}>
              {t('templates.title')}
            </Button>
          </div>
          <Button variant="outline" size="sm" onClick={() => setMultiOpen(true)}>
            {t('multi.open')}
          </Button>
          <Input
            type="search"
            className="w-full sm:w-56"
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
              const k = tone(b, me, now);
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
                  void loadOlder()
                    .catch(() => undefined)
                    .finally(() => setOlder(false));
                }}
              >
                {t('console.older')}
              </Button>
            )}
            {buf.cursor && buf.cmds.length >= MAX_COMMANDS && (
              <p className="mb-2 text-sm text-muted">{t('console.olderInHistory')}</p>
            )}
            {history.isError && (
              <div role="alert" className="mb-2 flex items-center gap-2 text-sm text-danger">
                {t('console.historyFailed')}
                <Button variant="ghost" size="sm" onClick={() => void history.refetch()}>
                  {t('console.retry')}
                </Button>
              </div>
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
                        now={now}
                        b={b}
                        t0={tone(b, me, now)}
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
        <form
          onSubmit={submit}
          className="relative border-t border-border bg-surface p-3 max-md:sticky max-md:bottom-0 max-md:z-10"
        >
          {suggestions.length > 0 && (
            <ul
              aria-label={t('console.suggestions')}
              data-scrolls
              className="absolute bottom-full left-3 mb-1 w-72 max-w-[calc(100%-1.5rem)] rounded-md border border-border bg-surface-raised py-1 max-md:static max-md:mb-2 max-md:flex max-md:w-full max-md:max-w-full max-md:gap-2 max-md:overflow-x-auto max-md:border-0 max-md:bg-transparent max-md:py-0"
            >
              {suggestions.map((s, i) => (
                <li key={s} className="max-md:shrink-0">
                  <button
                    type="button"
                    className={cn(
                      'flex w-full justify-between px-3 py-1.5 text-left font-mono text-sm max-md:min-h-11 max-md:items-center max-md:rounded-md max-md:border max-md:border-border max-md:whitespace-nowrap',
                      i === 0 && 'bg-surface',
                    )}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      setInput(`${s} `);
                    }}
                  >
                    {s}
                    {i === 0 && (
                      <span className="text-xs text-muted max-md:hidden">{t('console.tab')}</span>
                    )}
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
          <SlashHint paths={paths} input={input} />
          <p className="mt-1 text-xs text-muted max-md:hidden">{t('console.hint')}</p>
        </form>
      </section>
      {multiOpen && <MultiCommand current={id} open onClose={() => setMultiOpen(false)} />}
      <TemplatesPanel
        serverId={id}
        isPlayer={playerRule(allowed.data?.player_pattern)}
        open={templatesOpen}
        onClose={() => setTemplatesOpen(false)}
        onUse={(command) => {
          setInput(command);
          setWalk(-1);
        }}
      />
    </div>
  );
}
