// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import { useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useServer } from '@/api/live-servers';
import { useAllowedCommands, usePlayers } from '@/api/servers';
import { useTemplates } from '@/api/templates';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  buildCommand,
  fieldError,
  formErrors,
  FREE,
  fromCatalog,
  matches,
  pattern,
  playerRule,
  type Command,
  type Param,
  type Values,
} from '@/lib/commandform';
import type { CmdBlock } from '@/lib/console';
import { ago, clock, useNow } from '@/lib/format';
import { usePhone } from '@/lib/phone';
import { stripCodes } from '@/lib/serverpage';
import { fillTemplate, matchesTemplate, templateParams, type Template } from '@/lib/templates';
import { cn } from '@/lib/utils';
import { useConsoleFeed } from './useConsoleFeed';

// Reason codes with their own text (Design 3.5).
const reasons = [
  'not_in_list',
  'wrapper_blocked',
  'rate_limited',
  'invalid',
  'server_unavailable',
  'timeout',
  'busy',
];

interface Active {
  path: string;
  param: string;
}

export function Field({
  p,
  value,
  error,
  players,
  listId,
  onChange,
  onFocus,
}: {
  p: Param;
  value: string;
  error?: string;
  players: string[];
  listId: string;
  onChange: (v: string) => void;
  onFocus: () => void;
}) {
  const { t } = useTranslation();
  const id = useId();
  const label = p.optional ? t('commands.optional', { name: p.name }) : p.name;
  return (
    <div className="flex min-w-0 flex-col gap-1">
      {p.type === 'choice' ? (
        <>
          <span id={id} className="text-sm text-muted">
            {label}
          </span>
          <div role="radiogroup" aria-labelledby={id} className="flex flex-wrap gap-1">
            {(p.values ?? []).map((v) => (
              <button
                key={v}
                type="button"
                role="radio"
                aria-checked={value === v}
                onClick={() => onChange(v)}
                className={cn(
                  'min-h-9 rounded-md border px-3 font-mono text-sm max-md:min-h-11',
                  value === v
                    ? 'border-accent bg-surface-raised text-text'
                    : 'border-control text-muted',
                )}
              >
                {v}
              </button>
            ))}
          </div>
        </>
      ) : (
        <>
          <label htmlFor={id} className="text-sm text-muted">
            {label}
          </label>
          <Input
            id={id}
            className="font-mono"
            type={p.type === 'number' ? 'number' : 'text'}
            min={p.min}
            max={p.max}
            list={p.type === 'player' && players.length > 0 ? listId : undefined}
            autoComplete="off"
            spellCheck={false}
            aria-invalid={!!error}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            onFocus={onFocus}
          />
        </>
      )}
      {error && value.trim() !== '' && <p className="text-xs text-danger">{t(error)}</p>}
    </div>
  );
}

export function CommandCard({
  c,
  values,
  players,
  listId,
  busy,
  isPlayer,
  onChange,
  onFocusPlayer,
  onRun,
  folded,
  onToggle,
}: {
  c: Command;
  values: Values;
  players: string[];
  listId: string;
  busy: boolean;
  isPlayer: (name: string) => boolean;
  onChange: (name: string, v: string) => void;
  onFocusPlayer: (name: string) => void;
  onRun: () => void;
  // On a phone one card is open; undefined means it cannot fold.
  folded?: boolean;
  onToggle?: () => void;
}) {
  const { t } = useTranslation();
  const errors = formErrors(c, values, isPlayer);
  const command = buildCommand(c, values);
  const ok = Object.keys(errors).length === 0;
  const freeId = useId();
  const title = (
    <div className="min-w-0">
      <h3 className="font-semibold">{fromCatalog(c) ? c.description : c.path}</h3>
      <p className="font-mono text-sm text-muted">{pattern(c)}</p>
    </div>
  );
  return (
    <form
      aria-label={c.path}
      onSubmit={(e) => {
        e.preventDefault();
        if (ok && !busy) onRun();
      }}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
    >
      {onToggle ? (
        <button
          type="button"
          aria-expanded={!folded}
          onClick={onToggle}
          className="flex min-h-11 items-center justify-between gap-3 text-left"
        >
          {title}
          <ChevronRight aria-hidden className={cn('size-4 shrink-0', !folded && 'rotate-90')} />
        </button>
      ) : (
        title
      )}
      {folded ? null : fromCatalog(c) ? (
        c.params.length > 0 && (
          <div className="grid gap-3 sm:grid-cols-2">
            {c.params.map((p) => (
              <Field
                key={p.name}
                p={p}
                value={values[p.name] ?? ''}
                error={errors[p.name]}
                players={players}
                listId={listId}
                onChange={(v) => onChange(p.name, v)}
                onFocus={() => p.type === 'player' && onFocusPlayer(p.name)}
              />
            ))}
          </div>
        )
      ) : (
        <div className="flex flex-col gap-1">
          <label htmlFor={freeId} className="text-sm text-muted">
            {t('commands.args')}
          </label>
          <Input
            id={freeId}
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
            value={values[FREE] ?? ''}
            onChange={(e) => onChange(FREE, e.target.value)}
          />
        </div>
      )}
      {!folded && (
        <>
          <p
            className="wrap-anywhere md:truncate rounded bg-surface-raised px-2 py-1 font-mono text-sm"
            aria-live="polite"
          >
            {t('commands.preview', { command })}
          </p>
          <Button type="submit" className="self-start max-md:self-stretch" disabled={!ok || busy}>
            {t('commands.run')}
          </Button>
        </>
      )}
    </form>
  );
}

// TemplateCard fills a command template (CMD-008, D-068): one field per
// placeholder; the filled command goes through the pipeline like a typed
// one, so the fields are a help, not a check.
export function TemplateCard({
  tmpl,
  values,
  players,
  listId,
  busy,
  isPlayer,
  action,
  onChange,
  onFocusPlayer,
  onRun,
  children,
}: {
  tmpl: Template;
  values: Values;
  players: string[];
  listId: string;
  busy: boolean;
  isPlayer: (name: string) => boolean;
  action: string;
  onChange: (name: string, v: string) => void;
  onFocusPlayer: (name: string) => void;
  onRun: (command: string) => void;
  children?: ReactNode;
}) {
  const { t } = useTranslation();
  const params = templateParams(tmpl);
  const errors: Record<string, string> = {};
  for (const p of params) {
    const e = fieldError(p, values[p.name] ?? '', isPlayer);
    if (e) errors[p.name] = e;
  }
  const ok = Object.keys(errors).length === 0;
  const command = fillTemplate(tmpl.template, values);
  return (
    <form
      aria-label={tmpl.name}
      onSubmit={(e) => {
        e.preventDefault();
        if (ok && !busy) onRun(command);
      }}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="font-semibold wrap-anywhere">{tmpl.name}</h3>
          <p className="font-mono text-sm wrap-anywhere text-muted">{tmpl.template}</p>
        </div>
        {children}
      </div>
      {params.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2">
          {params.map((p) => (
            <Field
              key={p.name}
              p={p}
              value={values[p.name] ?? ''}
              error={errors[p.name]}
              players={players}
              listId={listId}
              onChange={(v) => onChange(p.name, v)}
              onFocus={() => p.type === 'player' && onFocusPlayer(p.name)}
            />
          ))}
        </div>
      )}
      <p
        className="wrap-anywhere md:truncate rounded bg-surface-raised px-2 py-1 font-mono text-sm"
        aria-live="polite"
      >
        {t('commands.preview', { command })}
      </p>
      <Button type="submit" className="self-start max-md:self-stretch" disabled={!ok || busy}>
        {action}
      </Button>
    </form>
  );
}

function HistoryItem({ b }: { b: CmdBlock }) {
  const { t, i18n } = useTranslation();
  // Answers are foreign data: text only, color codes dropped (SEC-006).
  const answer = b.response ? stripCodes(b.response) : '';
  const why =
    b.result === 'ok'
      ? ''
      : b.reason === 'rate_limited' && b.retryAfter
        ? t('commands.reason.rate_limited_wait', { s: b.retryAfter })
        : t(
            reasons.includes(b.reason ?? '')
              ? `commands.reason.${b.reason}`
              : 'commands.reason.other',
          );
  return (
    <li className="flex flex-col gap-1 border-t border-border px-4 py-2.5 text-sm first:border-t-0">
      <div className="flex items-baseline gap-2">
        <time dateTime={b.ts} className="font-mono text-xs text-muted">
          {clock(i18n.language, b.ts)}
        </time>
        <code className="min-w-0 flex-1 wrap-anywhere md:truncate font-mono">
          <ForeignText text={b.command} />
        </code>
        <span className={b.result === 'ok' ? 'text-ok' : 'text-danger'}>
          {t(`history.result.${b.result}`)}
        </span>
      </div>
      {answer && (
        <pre className="font-mono text-xs whitespace-pre-wrap text-muted">
          <ForeignText text={answer} />
        </pre>
      )}
      {why && (
        <p className="text-xs text-danger">
          {why}
          {b.detail && <span className="ml-1 font-mono">({b.detail})</span>}
        </p>
      )}
    </li>
  );
}

// CommandsTab is the command page of moderator and supporter (CMD-003):
// the commands of their list as cards with fields from the game profile,
// the players online to pick from, and their own history.
// A form belongs to its server: the key drops what was typed on a switch.
export function CommandsTab() {
  return <ServerCommands key={useServer()?.id} />;
}

function ServerCommands() {
  const { t } = useTranslation();
  const server = useServer();
  const id = server?.id ?? '';
  const allowed = useAllowedCommands(id, !!id);
  const templates = useTemplates(id);
  const players = usePlayers(id);
  const { buf, me, send } = useConsoleFeed(id, false);
  const now = useNow();
  const listId = useId();
  const [values, setValues] = useState<Record<string, Values>>({});
  const [active, setActive] = useState<Active | null>(null);
  const phone = usePhone();
  // The open card on a phone; null when all are folded.
  const [open, setOpen] = useState<string | null>();
  const [sheet, setSheet] = useState(true);
  const [search, setSearch] = useState('');
  if (!server) return null;

  // Color codes are no part of a name (SEC-006).
  const names = (players.data?.players ?? []).map(stripCodes);
  const isPlayer = playerRule(allowed.data?.player_pattern);
  const set = (path: string, name: string, v: string) =>
    setValues((all) => ({ ...all, [path]: { ...all[path], [name]: v } }));
  const commands = allowed.data?.commands ?? [];
  // A command without fields has nothing to fold. The first card with
  // fields is open until someone picks another.
  const folds = (c: Command) => !fromCatalog(c) || c.params.length > 0;
  const shown = commands.filter((c) => matches(c, search));
  // Templates share the values and the player picking of the cards under
  // their own key.
  const tmplKey = (tid: string) => `template:${tid}`;
  const shownTemplates = (templates.data ?? []).filter((x) => matchesTemplate(x, search));
  const openPath =
    open === null || shown.some((c) => c.path === open) ? open : shown.find(folds)?.path;
  const head = (
    <>
      {t('commands.players')}
      <span className="font-mono text-muted">
        {t('card.playersValue', { count: names.length })}
      </span>
    </>
  );
  const mine = buf.cmds
    .filter((c) => c.userId === me)
    .slice(-20)
    .reverse();

  return (
    <div className="grid gap-4 max-md:pb-44 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <datalist id={listId}>
        {names.map((n) => (
          <option key={n} value={n} />
        ))}
      </datalist>
      <div className="flex min-w-0 flex-col gap-4">
        <section aria-label={t('server.tab.commands')} className="min-w-0">
          {allowed.isPending && <p className="text-muted">{t('app.loading')}</p>}
          {allowed.isSuccess && commands.length === 0 && (
            <p className="text-muted">{t('commands.none')}</p>
          )}
          {commands.length > 0 && (
            <Input
              type="search"
              className="mb-3"
              placeholder={t('commands.search')}
              aria-label={t('commands.search')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          )}
          {commands.length > 0 && shown.length === 0 && shownTemplates.length === 0 && (
            <p className="text-muted">{t('commands.noMatch')}</p>
          )}
          <div className="grid gap-3 md:grid-cols-2">
            {shown.map((c) => (
              <CommandCard
                key={c.path}
                c={c}
                values={values[c.path] ?? {}}
                players={names}
                listId={listId}
                busy={send.isPending}
                isPlayer={isPlayer}
                onChange={(name, v) => set(c.path, name, v)}
                onFocusPlayer={(param) => setActive({ path: c.path, param })}
                onRun={() => send.mutate(buildCommand(c, values[c.path] ?? {}))}
                folded={phone && folds(c) ? openPath !== c.path : undefined}
                onToggle={
                  phone && folds(c) ? () => setOpen(openPath === c.path ? null : c.path) : undefined
                }
              />
            ))}
          </div>
        </section>
        {shownTemplates.length > 0 && (
          <section aria-label={t('templates.title')}>
            <h2 className="mb-2 text-sm font-semibold">{t('templates.title')}</h2>
            <div className="grid gap-3 md:grid-cols-2">
              {shownTemplates.map((x) => (
                <TemplateCard
                  key={x.id}
                  tmpl={x}
                  values={values[tmplKey(x.id)] ?? {}}
                  players={names}
                  listId={listId}
                  busy={send.isPending}
                  isPlayer={isPlayer}
                  action={t('commands.run')}
                  onChange={(name, v) => set(tmplKey(x.id), name, v)}
                  onFocusPlayer={(param) => setActive({ path: tmplKey(x.id), param })}
                  onRun={(command) => send.mutate(command)}
                />
              ))}
            </div>
          </section>
        )}
      </div>
      <div className="flex min-w-0 flex-col gap-4">
        {/* A sheet at the lower edge on a phone (Design 6.6). */}
        <section
          aria-label={t('commands.players')}
          className="rounded-lg border border-border bg-surface p-4 max-md:fixed max-md:inset-x-0 max-md:bottom-0 max-md:z-30 max-md:rounded-b-none max-md:pt-1 max-md:shadow-lg"
        >
          <h2 className="mb-3 text-sm font-semibold max-md:mb-1">
            {phone ? (
              <button
                type="button"
                aria-expanded={sheet}
                onClick={() => setSheet((s) => !s)}
                className="flex min-h-11 w-full items-center justify-between"
              >
                {head}
              </button>
            ) : (
              <span className="flex justify-between">{head}</span>
            )}
          </h2>
          {!sheet && phone ? null : (
            <>
              {players.data && !players.data.available && (
                <p className="text-sm text-muted">{t('overview.noPlayerList')}</p>
              )}
              {players.data?.available && names.length === 0 && (
                <p className="text-sm text-muted">{t('overview.noPlayers')}</p>
              )}
              <ul data-scrolls className="flex gap-2 max-md:overflow-x-auto md:flex-wrap">
                {names.map((n) => {
                  // Only a name the backend takes as player can be picked.
                  const usable = isPlayer(n);
                  return (
                    <li key={n} className="max-md:shrink-0">
                      <button
                        type="button"
                        disabled={!usable || !active}
                        title={t('commands.pick')}
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => active && set(active.path, active.param, n)}
                        className={cn(
                          'min-h-9 rounded-md border px-2.5 font-mono text-sm max-md:min-h-11',
                          active && values[active.path]?.[active.param] === n
                            ? 'border-accent text-text'
                            : 'border-control',
                        )}
                      >
                        <bdi>{n}</bdi>
                      </button>
                    </li>
                  );
                })}
              </ul>
              <p className="mt-3 text-xs text-muted">
                {t('commands.pickHint')}
                {players.data?.updated_at &&
                  ` · ${t('overview.playersAt', { ago: ago(t, players.data.updated_at, now) })}`}
              </p>
            </>
          )}
        </section>
        <section
          aria-label={t('commands.myHistory')}
          className="rounded-lg border border-border bg-surface"
        >
          <h2 className="px-4 pt-3 pb-2 text-sm font-semibold">{t('commands.myHistory')}</h2>
          {mine.length === 0 ? (
            <p className="px-4 pb-3 text-sm text-muted">{t('history.empty')}</p>
          ) : (
            <ol>
              {mine.map((b) => (
                <HistoryItem key={b.id} b={b} />
              ))}
            </ol>
          )}
          <div className="border-t border-border px-4 py-2.5">
            <Link
              to="/servers/$serverId/history"
              params={{ serverId: id }}
              className="tap text-sm text-accent underline"
            >
              {t('commands.showAll')}
            </Link>
          </div>
        </section>
      </div>
    </div>
  );
}
