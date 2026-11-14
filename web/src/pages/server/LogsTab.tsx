// SPDX-License-Identifier: AGPL-3.0-or-later
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import { useServer } from '@/api/live-servers';
import { useSession } from '@/api/session';
import type { Server } from '@/api/servers';
import { CopyButton } from '@/components/CopyButton';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { compileFilter, LOG_OPS, MAX_FILTER_LENGTH, type LogOp } from '@/lib/logfilter';
import { bufferWriter, MAX_LOG_LINES, prependTail, updateBuffer, useBuffer } from '@/lib/console';
import { stripCodes } from '@/lib/serverpage';
import { cn } from '@/lib/utils';
import { useLogsFeed } from './useConsoleFeed';

// The next request for earlier lines asks this many more (W8 A).
const STEP = 200;

// LogsTab shows the container logs of a server (UI-009): live while the
// tab is open, earlier lines on demand over logs.tail (revision 2.4
// candidate 20). The lines share the buffer with the console's log track.
export function LogsTab() {
  const server = useServer();
  return server ? <LogsView server={server} /> : null;
}

// LogsView is the tab for a given server.
export function LogsView({ server }: { server: Server }) {
  const { t } = useTranslation();
  const id = server.id;
  useLogsFeed(id, !!id);
  const buf = useBuffer(id);
  const [filter, setFilter] = useState('');
  // The two-part filter behind its flag (UI-019, NFR-019).
  const { data: session } = useSession();
  const split = session?.ui.features?.includes('log_filter_split') ?? false;
  const [bracket, setBracket] = useState('');
  const [rest, setRest] = useState('');
  const [op, setOp] = useState<LogOp>('and');
  const opId = useId();
  const [follow, setFollow] = useState(true);
  const [wrap, setWrap] = useState(true);
  const [earlier, setEarlier] = useState<'idle' | 'busy' | 'done'>('idle');
  const [problem, setProblem] = useState('');
  const box = useRef<HTMLDivElement>(null);
  const pre = useRef<HTMLPreElement>(null);

  const all = buf.logs.flatMap((l) => l.lines.map(stripCodes));
  const q = filter.trim().toLowerCase();
  const compiled = split ? compileFilter({ bracket, text: rest, op }) : undefined;
  // A part the filter refuses filters nothing; the hint says why.
  const splitTest = compiled?.ok ? compiled.test : undefined;
  const shown = split
    ? splitTest
      ? all.filter(splitTest)
      : all
    : q
      ? all.filter((l) => l.toLowerCase().includes(q))
      : all;
  const text = shown.join('\n');

  // Follow keeps the end in view; scrolling up ends it (Design 6.1).
  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [follow, text]);

  const loadEarlier = async () => {
    setEarlier('busy');
    setProblem('');
    const want = Math.min(MAX_LOG_LINES, all.length + STEP);
    const write = bufferWriter();
    try {
      const tail = must(
        await api.GET('/servers/{id}/logs', { params: { path: { id }, query: { lines: want } } }),
      );
      write(id, (b) => prependTail(b, tail.lines));
      // Fewer lines than asked: the log has no more.
      setEarlier(tail.lines.length < want || want >= MAX_LOG_LINES ? 'done' : 'idle');
    } catch (e) {
      const err = e instanceof ApiError ? e : undefined;
      const reason = err?.problem?.reason ?? '';
      setProblem(
        reason === 'rate_limited'
          ? t('logs.rateLimited', { s: err?.retryAfter ?? 6 })
          : reason === 'agent_unavailable'
            ? t('logs.agentGone')
            : reason === 'agent_unsupported'
              ? t('logs.unsupported')
              : t('logs.failed'),
      );
      setEarlier('idle');
    }
  };

  const agentGone = server.runtime?.container?.agent_connected === false;
  return (
    <section
      aria-label={t('server.tab.logs')}
      className="flex flex-col rounded-lg border border-border bg-surface"
    >
      {/* Two buttons a row on a phone, the filter below them. */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border p-3 max-md:grid max-md:grid-cols-2">
        <Button
          variant="outline"
          size="sm"
          aria-pressed={follow}
          onClick={() => setFollow((f) => !f)}
        >
          {t('logs.follow')}
        </Button>
        <Button variant="outline" size="sm" aria-pressed={wrap} onClick={() => setWrap((w) => !w)}>
          {t('logs.wrap')}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => updateBuffer(id, (b) => ({ ...b, logs: [] }))}
        >
          {t('logs.clear')}
        </Button>
        <CopyButton text={text} target={pre} label={t('logs.copy')} />
        {split ? (
          <div className="ml-auto flex w-full flex-wrap items-center gap-2 max-md:col-span-2 md:w-auto">
            <Input
              type="search"
              className="w-full md:w-48"
              placeholder={t('logs.split.bracketHint')}
              aria-label={t('logs.split.bracket')}
              value={bracket}
              onChange={(e) => setBracket(e.target.value)}
            />
            <label htmlFor={opId} className="sr-only">
              {t('logs.split.op')}
            </label>
            <select
              id={opId}
              className="h-9 rounded-md border border-control bg-surface px-2 text-sm max-md:h-11"
              value={op}
              onChange={(e) => setOp(e.target.value as LogOp)}
            >
              {LOG_OPS.map((o) => (
                <option key={o} value={o}>
                  {t(`logs.split.ops.${o}`)}
                </option>
              ))}
            </select>
            <Input
              type="search"
              className="w-full md:w-48"
              placeholder={t('console.filter')}
              aria-label={t('logs.split.text')}
              value={rest}
              onChange={(e) => setRest(e.target.value)}
            />
          </div>
        ) : (
          <Input
            type="search"
            className="ml-auto w-full max-md:col-span-2 md:w-56"
            placeholder={t('console.filter')}
            aria-label={t('logs.filterLabel')}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        )}
      </div>
      {compiled && !compiled.ok && (
        <p role="status" className="border-b border-border px-3 py-2 text-sm text-warn">
          {t(`logs.split.problem.${compiled.problem}`, { max: MAX_FILTER_LENGTH })}
        </p>
      )}
      {agentGone && (
        <p className="border-b border-border px-3 py-2 text-sm text-warn">{t('logs.agentGone')}</p>
      )}
      <div
        ref={box}
        className="h-[60vh] min-h-72 overflow-auto px-3 py-2"
        onScroll={(e) => {
          const el = e.currentTarget;
          if (el.scrollHeight - el.scrollTop - el.clientHeight > 40) setFollow(false);
        }}
      >
        {earlier !== 'done' && all.length < MAX_LOG_LINES && (
          <Button
            variant="ghost"
            size="sm"
            className="mb-2"
            disabled={earlier === 'busy'}
            onClick={() => void loadEarlier()}
          >
            {t('logs.earlier')}
          </Button>
        )}
        {problem && (
          <p role="alert" className="mb-2 text-sm text-danger">
            {problem}
          </p>
        )}
        {shown.length === 0 ? (
          <p className="text-sm text-muted">{t(all.length ? 'logs.noMatch' : 'logs.empty')}</p>
        ) : (
          <pre
            ref={pre}
            aria-label={t('logs.lines')}
            className={cn(
              'font-mono text-xs',
              wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre',
            )}
          >
            <ForeignText text={text} />
          </pre>
        )}
      </div>
    </section>
  );
}
