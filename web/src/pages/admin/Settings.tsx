// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { api, must, patchVersioned } from '@/api/client';
import { sessionKey } from '@/api/session';
import { outcome } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { useWide } from '@/components/DenseTable';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { formatDuration, isGoDuration, parseDuration } from '@/lib/duration';
import { usePhone } from '@/lib/phone';
import { cn } from '@/lib/utils';
import { useUndo, useUnsaved } from '@/lib/undo';
import { PhoneNote } from './PhoneNote';
import { LogMaskingPanel } from './LogMaskingPanel';
import { SmtpPanel } from './SmtpPanel';
import { PageHeader } from '@/components/PageHeader';

type Values = Record<string, unknown>;
export const settingsKey = ['settings'] as const;

// Whether the explanations show is a per-browser convenience (UI-014).
const helpKey = 'rcd.settings.help';
function storedHelp() {
  try {
    return localStorage.getItem(helpKey) !== 'off';
  } catch {
    return true;
  }
}
function storeHelp(on: boolean) {
  try {
    localStorage.setItem(helpKey, on ? 'on' : 'off');
  } catch {
    // private mode: the choice lasts for this page only
  }
}

// Keys with their own label and help in the language file.
const keyTexts = [
  'features_profile_browser',
  'features_color_codes',
  'features_say_sender',
  'features_docker_host',
  'features_server_number',
  'features_messages',
  'features_diagnostics_export',
  'features_host_groups',
  'features_log_masking_ui',
  'features_server_save_as_new',
  'features_log_filter_split',
  'features_command_palette',
  'auth_recovery_lifetime',
  'auth_recovery_per_hour',
  'auth_recovery_queue',
  'auth_recovery_workers',
  'auth_second_factor_window',
  'auth_totp_required',
  'agent_version_retry',
  'agent_version_window',
  'agent_volume_interval',
  'agent_volume_stale',
  'agents_display_name_max',
  'agents_fingerprint_pairing',
  'audit_retention_days',
  'backup_interval',
  'backup_keep_auto',
  'backup_keep_pre_migration',
  'commands_multi_max_servers',
  'commands_multi_parallel',
  'commands_templates_per_server',
  'diagnostics_max_bytes',
  'diagnostics_parallel',
  'invite_lifetime',
  'login_lockout_duration',
  'login_max_client_failures',
  'login_max_failures',
  'login_max_tracked',
  'login_overflow_gap',
  'login_overflow_line',
  'messages_comments_per_hour',
  'messages_max_comment',
  'messages_max_recipients',
  'messages_max_resolved_recipients',
  'messages_max_text',
  'messages_per_hour',
  'messages_retention_days',
  'profiles_poll_interval',
  'servers_connection_test_per_minute',
  'servers_http_parallel',
  'session_idle_timeout',
  'session_max_lifetime',
  'smtp_test_per_minute',
  'smtp_timeout',
  'supporters_enabled',
  'system_log_max_bytes',
  'ui_shortcuts_enabled',
  'ws_max_sockets_per_session',
  'ws_max_subscriptions_per_socket',
];

// Groups of the settings with their sections, in this order (Design 5.6);
// keys not listed show under "other", so a new key is never hidden.
const groupOrder = [
  'security',
  'users',
  'messages',
  'servers',
  'agents',
  'system',
  'features',
  'other',
] as const;
type Group = (typeof groupOrder)[number];
const sections: [string, Group, RegExp][] = [
  ['login', 'security', /^login\./],
  ['twofactor', 'security', /^auth\./],
  ['sessions', 'security', /^session\./],
  ['invitations', 'users', /^invite\./],
  ['supporters', 'users', /^supporters\./],
  ['messages', 'messages', /^messages\./],
  ['commands', 'servers', /^(commands\.|servers\.http_parallel$)/],
  ['ratelimits', 'servers', /^(ratelimit\.|servers\.)/],
  ['profiles', 'servers', /^profiles\./],
  ['websocket', 'servers', /^ws\./],
  ['agents', 'agents', /^agents?\./],
  ['audit', 'system', /^audit\./],
  ['backups', 'system', /^backup\./],
  ['mail', 'system', /^smtp\./],
  ['interface', 'system', /^ui\./],
  ['diagnostics', 'system', /^(diagnostics|system_log)\./],
  ['features', 'features', /^features\./],
];
const sectionOrder = [...sections.map(([name]) => name), 'other'];
// Internal tuning values: they stay behind "Advanced", closed at first.
const advancedKeys = new Set([
  'login.max_tracked',
  'login.overflow_gap',
  'login.overflow_line',
  'auth.recovery_workers',
  'auth.recovery_queue',
  'commands.multi_parallel',
  'servers.http_parallel',
  'profiles.poll_interval',
  'ws.max_subscriptions_per_socket',
  'agent.version_retry',
  'agent.version_window',
  'agent.volume_interval',
  'agent.volume_stale',
  'diagnostics.parallel',
]);
// describe gives the label and help of a key from the language file
// (NFR-007); rate limits share one text per kind with the role in it. A
// key without texts shows by its name.
function describe(t: TFunction, key: string) {
  const rate = /^ratelimit\.(\w+)\.(per_minute|burst)$/.exec(key);
  if (rate) {
    const role = t(`role.${rate[1]}`);
    return {
      label: t(`settings.key.ratelimit_${rate[2]}.label`, { role }),
      help: t(`settings.key.ratelimit_${rate[2]}.help`, { role }),
    };
  }
  const slug = key.replace(/\./g, '_');
  const known = keyTexts.includes(slug);
  return {
    label: known ? t(`settings.key.${slug}.label`) : key,
    help: known ? t(`settings.key.${slug}.help`) : '',
  };
}

const sectionOf = (key: string) => sections.find(([, , re]) => re.test(key))?.[0] ?? 'other';
const groupOf = (section: string): Group =>
  sections.find(([name]) => name === section)?.[1] ?? 'other';

// The chosen group is a per-browser convenience, kept over a reload.
const groupKey = 'rcd.settings.group';
function storedGroup(): Group {
  try {
    const g = localStorage.getItem(groupKey);
    return groupOrder.find((x) => x === g) ?? 'security';
  } catch {
    return 'security';
  }
}
function storeGroup(g: Group) {
  try {
    localStorage.setItem(groupKey, g);
  } catch {
    // private mode: the choice lasts for this page only
  }
}

const settingsQuery = {
  queryKey: settingsKey,
  queryFn: async () => {
    const r = await api.GET('/settings');
    return { values: must(r) as Values, etag: r.response.headers.get('ETag') ?? '' };
  },
};

// SettingsPage changes the global settings (Design 5.6): only the RCON
// admin. Changes wait for undo and go out with the settings' ETag
// (revision 2.4 candidate 24); switching supporters off asks first, as
// it ends the sessions of all supporters. A phone only reads (UI-012).
// SettingsPage holds the global settings, the SMTP setup (NFR-017) and,
// the masking of container logs (UI-022).
// From 1024 px the groups stand on the left and only the chosen one on the
// right (O8); below, all groups follow one another as before.
export function SettingsPage() {
  const { t } = useTranslation();
  const phone = usePhone();
  const wide = useWide();
  const [showHelp, setShowHelp] = useState(storedHelp);
  const [chosen, setChosen] = useState<Group>(storedGroup);
  const settings = useQuery(settingsQuery);
  const present = new Set(
    Object.keys(settings.data?.values ?? {}).map((k) => groupOf(sectionOf(k))),
  );
  // The mail and masking panels belong to the system group.
  const groups = groupOrder.filter((g) => g === 'system' || present.has(g));
  const group = wide ? (groups.includes(chosen) ? chosen : 'security') : null;
  const system = group === null || group === 'system';

  const head = (
    <PageHeader title={t('nav.settings')} description={t('settings.explain')}>
      {!phone && (
        <label className="mt-2 flex min-h-11 items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="max-md:size-11"
            checked={showHelp}
            onChange={(e) => {
              setShowHelp(e.target.checked);
              storeHelp(e.target.checked);
            }}
          />
          {t('settings.showHelp')}
        </label>
      )}
    </PageHeader>
  );
  const content = (
    <div className="flex min-w-0 flex-col gap-5">
      <SettingsForm group={group} showHelp={showHelp} />
      {system && <SmtpPanel />}
      {system && <LogMaskingPanel />}
    </div>
  );
  if (!wide) {
    return (
      <div className="flex flex-col gap-5">
        {head}
        {content}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-5">
      {head}
      <div className="grid grid-cols-[13rem_minmax(0,1fr)] items-start gap-6">
        <nav aria-label={t('settings.groupsLabel')} className="flex flex-col gap-1">
          {groups.map((g) => (
            <button
              key={g}
              type="button"
              aria-current={g === group}
              className={cn(
                'rounded-md px-3 py-1.5 text-left text-sm hover:bg-surface-raised',
                g === group && 'bg-surface-raised font-semibold',
              )}
              onClick={() => {
                setChosen(g);
                storeGroup(g);
              }}
            >
              {t(`settings.group.${g}`)}
            </button>
          ))}
        </nav>
        {content}
      </div>
    </div>
  );
}

// SettingsForm holds the changes of all groups; it shows one group, or all
// of them when group is null.
function SettingsForm({ group, showHelp }: { group: Group | null; showHelp: boolean }) {
  const { t } = useTranslation();
  const phone = usePhone();
  const qc = useQueryClient();
  const { saved, saving } = useUndo();
  const settings = useQuery(settingsQuery);
  const [draft, setDraft] = useState<Values | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [conflict, setConflict] = useState(false);
  const [confirmOff, setConfirmOff] = useState(false);
  // What people typed into duration fields, shown as typed.
  const [texts, setTexts] = useState<Record<string, string>>({});
  const reset = () => {
    setDraft(null);
    setTexts({});
  };
  const base = settings.data;
  const values = draft ?? base?.values ?? {};
  // Durations compare by their length, not by their spelling.
  const same = (a: unknown, b: unknown) =>
    isGoDuration(a) && isGoDuration(b)
      ? formatDuration(a) === formatDuration(b)
      : JSON.stringify(a) === JSON.stringify(b);
  const changed = Object.keys(values).filter((k) => base && !same(values[k], base.values[k]));
  useUnsaved(changed.length > 0);
  const off = conflict || phone;
  const invalid = Object.entries(texts).some(([, x]) => parseDuration(x) === null);

  // A change goes out at once; Undo sends the old values (D-048).
  const write = async (body: Values, etag: string) => {
    let next = '';
    const r = await saving(
      outcome('settings', async () => {
        const res = await patchVersioned('/settings', etag, { body });
        must(res);
        next = res.response.headers.get('ETag') ?? '';
      }),
    );
    if (r.reason === 'conflict') {
      setConflict(true);
      setMessage({ ok: false, text: t('admin.conflict') });
      return null;
    }
    setMessage(r.ok ? { ok: true, text: t('admin.saved') } : { ok: false, text: reasonText(t, r) });
    if (r.ok) reset();
    void qc.invalidateQueries({ queryKey: settingsKey });
    // ui.* and supporters.* reach the session (shortcuts, navigation).
    void qc.invalidateQueries({ queryKey: sessionKey });
    return r.ok ? next : null;
  };
  const send = async () => {
    if (!base) return;
    const pick = (from: Values) => Object.fromEntries(changed.map((k) => [k, from[k]]));
    const old = pick(base.values);
    const etag = await write(pick(values), base.etag);
    if (etag !== null) {
      saved(t('settings.undo', { count: changed.length }), () => void write(old, etag));
    }
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (changed.includes('supporters.enabled') && values['supporters.enabled'] === false)
      setConfirmOff(true);
    else void send();
  };
  const set = (key: string, v: unknown) => {
    setMessage(null);
    setDraft({ ...values, [key]: v });
  };

  const keys = Object.keys(values).sort();
  const shownSections = sectionOrder.filter(
    (sec) => (group === null || groupOf(sec) === group) && keys.some((k) => sectionOf(k) === sec),
  );
  const shownGroups = groupOrder.filter((g) => shownSections.some((sec) => groupOf(sec) === g));

  const field = (k: string) => {
    const v =
      base?.values[k] !== undefined && isGoDuration(base.values[k]) ? 'duration' : typeof values[k];
    const id = `setting-${k}`;
    const { label, help } = describe(t, k);
    const head = (
      <span className="flex min-w-0 flex-col">
        <span className="font-medium">{label}</span>
        {showHelp && help && <span className="text-xs text-muted">{help}</span>}
        <code className="font-mono text-xs text-muted">{k}</code>
      </span>
    );
    if (v === 'boolean') {
      return (
        <label key={k} className="flex items-center justify-between gap-3 text-sm sm:col-span-2">
          {head}
          <input
            id={id}
            type="checkbox"
            className="size-5 shrink-0"
            disabled={off}
            checked={values[k] === true}
            onChange={(e) => set(k, e.target.checked)}
          />
        </label>
      );
    }
    const text =
      v === 'duration' ? (texts[k] ?? formatDuration(String(values[k]))) : String(values[k] ?? '');
    const bad = v === 'duration' && texts[k] !== undefined && parseDuration(texts[k]) === null;
    return (
      <div key={k} className="flex flex-col gap-1 text-sm">
        <label htmlFor={id}>{head}</label>
        <Input
          id={id}
          type={v === 'number' ? 'number' : 'text'}
          inputMode={v === 'duration' ? 'text' : undefined}
          disabled={off}
          aria-invalid={bad}
          value={text}
          onChange={(e) => {
            if (v === 'duration') {
              const raw = e.target.value;
              setTexts((x) => ({ ...x, [k]: raw }));
              const go = parseDuration(raw);
              if (go) set(k, go);
            } else set(k, v === 'number' ? Number(e.target.value) : e.target.value);
          }}
        />
        {bad && <span className="text-xs text-danger">{t('settings.badDuration')}</span>}
      </div>
    );
  };
  const section = (sec: string) => {
    const own = keys.filter((k) => sectionOf(k) === sec);
    const plain = own.filter((k) => !advancedKeys.has(k));
    const tuning = own.filter((k) => advancedKeys.has(k));
    return (
      <section
        key={sec}
        aria-label={t(`settings.section.${sec}`)}
        className="rounded-lg border border-border bg-surface p-4"
      >
        <h3 className="mb-3 font-semibold">{t(`settings.section.${sec}`)}</h3>
        {plain.length > 0 && <div className="grid gap-3 sm:grid-cols-2">{plain.map(field)}</div>}
        {tuning.length > 0 && (
          <details className={plain.length > 0 ? 'mt-3' : undefined}>
            <summary className="cursor-pointer text-sm text-muted max-md:py-3">
              {t('settings.advanced')}
            </summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">{tuning.map(field)}</div>
          </details>
        )}
      </section>
    );
  };

  return (
    <form onSubmit={submit} className="flex max-w-3xl flex-col gap-5">
      <PhoneNote />
      {settings.isPending && <p className="text-muted">{t('app.loading')}</p>}
      {shownGroups.map((g) => (
        <div key={g} className="flex flex-col gap-3">
          <h2 className="text-lg font-semibold">{t(`settings.group.${g}`)}</h2>
          {shownSections.filter((sec) => groupOf(sec) === g).map(section)}
        </div>
      ))}
      {message && (
        <p
          role={message.ok ? 'status' : 'alert'}
          className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
        >
          {message.text}
        </p>
      )}
      <div className="flex gap-2">
        {!off && (
          <Button type="submit" disabled={changed.length === 0 || invalid}>
            {t('settings.save', { count: changed.length })}
          </Button>
        )}
        {conflict && (
          <Button
            type="button"
            variant="outline"
            onClick={async () => {
              await qc.invalidateQueries({ queryKey: settingsKey });
              reset();
              setConflict(false);
              setMessage(null);
            }}
          >
            {t('admin.reload')}
          </Button>
        )}
        {changed.length > 0 && !off && (
          <Button type="button" variant="ghost" onClick={() => reset()}>
            {t('settings.discard')}
          </Button>
        )}
      </div>
      <ConfirmDialog
        open={confirmOff}
        title={t('settings.supportersOffTitle')}
        text={t('settings.supportersOffText')}
        action={t('settings.supportersOff')}
        onClose={() => setConfirmOff(false)}
        onConfirm={() => {
          setConfirmOff(false);
          void send();
        }}
      />
    </form>
  );
}
