// SPDX-License-Identifier: AGPL-3.0-or-later
import { useDeferredValue, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { components } from '@/api/schema';
import { ApiError } from '@/api/client';
import {
  useProfiles,
  useServerProfile,
  useServers,
  type EffectiveProfile,
  type GameProfile,
} from '@/api/servers';
import { ForeignText } from '@/components/ForeignText';
import { Input } from '@/components/ui/input';

type Param = components['schemas']['ProfileParam'];
type Port = components['schemas']['GamePort'];
type Command = { path: string; description: string; params: Param[] };

// Everything a game profile or a server carries is foreign data (SEC-006):
// it goes through ForeignText, never as markup.
function Foreign({ text }: { text: string }) {
  return (
    <code className="rounded border border-border px-1 font-mono text-xs whitespace-pre">
      <ForeignText text={text} />
    </code>
  );
}

// matches is a plain substring test on text already in lower case: no
// pattern is built from the input, so any input stays cheap (UI-020).
const matches = (text: string, q: string) => text.toLowerCase().includes(q);

function commandMatches(c: Command, q: string) {
  return matches(c.path, q) || matches(c.description, q);
}

function Values({ list }: { list: string[] }) {
  const { t } = useTranslation();
  if (list.length === 0) return <span className="text-muted">{t('profiles.none')}</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {list.map((v, i) => (
        <Foreign key={i} text={v} />
      ))}
    </span>
  );
}

function ParamLine({ p }: { p: Param }) {
  const { t } = useTranslation();
  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-1">
      <Foreign text={p.name} />
      <span className="text-muted">{t(`profiles.type.${p.type}`)}</span>
      {p.optional && <span className="text-muted">{t('profiles.optional')}</span>}
      {p.min !== undefined && <span>{t('profiles.min', { value: p.min })}</span>}
      {p.max !== undefined && <span>{t('profiles.max', { value: p.max })}</span>}
      {p.values && p.values.length > 0 && (
        <>
          <span className="text-muted">{t('profiles.values')}</span>
          <Values list={p.values} />
        </>
      )}
    </li>
  );
}

// Catalog lists commands with their parameters; source names the profile
// an entry comes from, in the effective view.
function Catalog({
  commands,
  label,
  source,
}: {
  commands: Command[];
  label: string;
  source?: (i: number) => React.ReactNode;
}) {
  const { t } = useTranslation();
  if (commands.length === 0)
    return <p className="text-sm text-muted">{t('profiles.noCommands')}</p>;
  return (
    <ul aria-label={label} className="flex flex-col gap-2">
      {commands.map((c, i) => (
        <li
          key={`${c.path}-${i}`}
          data-testid="catalog-entry"
          className="flex flex-col gap-1 rounded-md border border-border p-2 text-sm"
        >
          <div className="flex flex-wrap items-center gap-2">
            <Foreign text={c.path} />
            {source && (
              <span className="text-xs text-muted">
                {t('profiles.from')} {source(i)}
              </span>
            )}
          </div>
          {c.description && (
            <p className="text-muted wrap-anywhere">
              <ForeignText text={c.description} />
            </p>
          )}
          {c.params.length > 0 && (
            <ul aria-label={t('profiles.params')} className="flex flex-col gap-1 pl-3 text-xs">
              {c.params.map((p, j) => (
                <ParamLine key={j} p={p} />
              ))}
            </ul>
          )}
        </li>
      ))}
    </ul>
  );
}

// Ports are shown only where the backend sent them (D-056).
function Ports({ ports }: { ports?: Port[] }) {
  const { t } = useTranslation();
  if (!ports) return null;
  return (
    <Field label={t('profiles.gamePorts')}>
      {ports.length === 0 ? (
        <span className="text-muted">{t('profiles.none')}</span>
      ) : (
        <ul aria-label={t('profiles.gamePorts')} className="flex flex-wrap gap-1.5">
          {ports.map((p, i) => (
            <li
              key={i}
              className="flex items-center gap-1.5 rounded-md border border-border px-2 py-0.5 font-mono text-xs"
            >
              <span className="text-muted">
                <ForeignText text={p.name} />
              </span>
              <span>
                {t('ports.value', {
                  port: p.port,
                  proto:
                    p.proto === 'tcp' || p.proto === 'udp' ? t(`ports.proto.${p.proto}`) : p.proto,
                })}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Field>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 sm:flex-row sm:gap-3">
      <dt className="text-muted sm:w-40 sm:shrink-0">{label}</dt>
      <dd className="min-w-0 wrap-anywhere">{children}</dd>
    </div>
  );
}

function Syntax({ p }: { p: GameProfile | EffectiveProfile }) {
  const { t } = useTranslation();
  return (
    <>
      <Field label={t('profiles.protocol')}>
        <Foreign text={p.protocol} />
      </Field>
      <Field label={t('profiles.caseSensitive')}>
        {t(p.case_sensitive ? 'profiles.yes' : 'profiles.no')}
      </Field>
      <Field label={t('profiles.stripPrefix')}>
        {p.strip_prefix ? (
          <Foreign text={p.strip_prefix} />
        ) : (
          <span className="text-muted">{t('profiles.none')}</span>
        )}
      </Field>
      <Field label={t('profiles.maxLength')}>{p.max_length}</Field>
      <Field label={t('profiles.wrappers')}>
        <Values list={p.wrappers} />
      </Field>
    </>
  );
}

function ProfileCard({ p, q }: { p: GameProfile; q: string }) {
  const { t } = useTranslation();
  // A match on the name shows the whole catalog, else only matching commands.
  const commands =
    !q || matches(p.name, q) ? p.catalog : p.catalog.filter((c) => commandMatches(c, q));
  return (
    <li data-testid="profile" className="rounded-lg border border-border bg-surface">
      <details open={q !== ''}>
        <summary className="tap flex cursor-pointer flex-wrap items-center gap-x-3 gap-y-1 p-3">
          <span className="font-semibold">
            <ForeignText text={p.name} />
          </span>
          <span className="text-sm text-muted">{t(`profiles.kind.${p.kind}`)}</span>
          <span className="text-sm text-muted">
            {t('profiles.commandCount', { count: p.catalog.length })}
          </span>
        </summary>
        <div className="flex flex-col gap-3 border-t border-border p-3">
          <dl className="flex flex-col gap-1 text-sm">
            <Syntax p={p} />
            {p.default_port !== undefined && (
              <Field label={t('profiles.defaultPort')}>{p.default_port}</Field>
            )}
            <Ports ports={p.game_ports} />
          </dl>
          <Catalog commands={commands} label={t('profiles.catalogOf', { name: p.name })} />
        </div>
      </details>
    </li>
  );
}

// Effective shows the merged profile of one server (SRV-008, D-074): the
// order, the syntax, wrappers, separators and the catalog with the source
// of each entry.
function Effective({ id, profiles }: { id: string; profiles: GameProfile[] }) {
  const { t } = useTranslation();
  const q = useServerProfile(id);
  const nameOf = (pid: string) => profiles.find((p) => p.id === pid)?.name ?? pid;
  if (q.isPending) return <p className="text-sm text-muted">{t('app.loading')}</p>;
  if (q.isError) {
    const gone = q.error instanceof ApiError && q.error.status === 404;
    return (
      <p role="status" className="text-sm text-muted">
        {t(gone ? 'profiles.effectiveMissing' : 'profiles.effectiveFailed')}
      </p>
    );
  }
  const e = q.data;
  return (
    <div data-testid="effective" className="flex flex-col gap-3">
      <dl className="flex flex-col gap-1 text-sm">
        <Field label={t('profiles.order')}>
          {e.profile_ids.length === 0 ? (
            <span className="text-muted">{t('profiles.none')}</span>
          ) : (
            <ol aria-label={t('profiles.order')} className="flex list-inside list-decimal flex-col">
              {e.profile_ids.map((pid) => (
                <li key={pid}>
                  <ForeignText text={nameOf(pid)} />
                </li>
              ))}
            </ol>
          )}
        </Field>
        <Syntax p={e} />
        <Field label={t('profiles.separators')}>
          <Values list={e.separators} />
        </Field>
        <Ports ports={e.game_ports} />
      </dl>
      <Catalog
        commands={e.catalog}
        label={t('profiles.effectiveCatalog')}
        source={(i) => <ForeignText text={nameOf(e.catalog[i]?.profile_id ?? '')} />}
      />
    </div>
  );
}

// ProfilesPage is the read-only profile browser (UI-020, flag
// profile_browser): all game profiles with their catalog and the effective
// profile of a server in scope. It writes nothing.
export function ProfilesPage() {
  const { t } = useTranslation();
  const profiles = useProfiles();
  const servers = useServers();
  const [query, setQuery] = useState('');
  const [server, setServer] = useState('');
  const pickId = useId();
  const q = useDeferredValue(query).toLowerCase();
  const all = profiles.data ?? [];
  const list = q
    ? all.filter((p) => matches(p.name, q) || p.catalog.some((c) => commandMatches(c, q)))
    : all;
  const inScope = servers.data ?? [];

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">{t('nav.profiles')}</h1>

      <section
        aria-label={t('profiles.effective')}
        className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
      >
        <h2 className="text-lg font-semibold">{t('profiles.effective')}</h2>
        {servers.isPending ? (
          <p className="text-sm text-muted">{t('app.loading')}</p>
        ) : inScope.length === 0 ? (
          <p className="text-sm text-muted">{t('home.empty')}</p>
        ) : (
          <>
            <label htmlFor={pickId} className="text-sm text-muted">
              {t('profiles.pickServer')}
            </label>
            <select
              id={pickId}
              className="h-11 self-start rounded-md border border-control bg-surface px-3 text-sm"
              value={server}
              onChange={(e) => setServer(e.target.value)}
            >
              <option value="">{t('profiles.pickServerOption')}</option>
              {inScope.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            {server && <Effective id={server} profiles={all} />}
          </>
        )}
      </section>

      <section aria-label={t('profiles.all')} className="flex flex-col gap-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <h2 className="text-lg font-semibold">{t('profiles.all')}</h2>
          <Input
            type="search"
            className="w-full sm:w-64"
            placeholder={t('profiles.search')}
            aria-label={t('profiles.searchLabel')}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        {profiles.isPending && <p className="text-muted">{t('app.loading')}</p>}
        {profiles.isError && <p className="text-danger">{t('profiles.failed')}</p>}
        {profiles.isSuccess && list.length === 0 && (
          <p className="text-muted">{t(q ? 'profiles.noMatch' : 'profiles.empty')}</p>
        )}
        {list.length > 0 && (
          <ul aria-label={t('profiles.all')} className="flex flex-col gap-2">
            {list.map((p) => (
              <ProfileCard key={p.id} p={p} q={q} />
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
