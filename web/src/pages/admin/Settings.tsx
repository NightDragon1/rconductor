// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { api, must, patchVersioned } from '@/api/client';
import { sessionKey } from '@/api/session';
import { outcome } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { formatDuration, isGoDuration, parseDuration } from '@/lib/duration';
import { usePhone } from '@/lib/phone';
import { useUndo, useUnsaved } from '@/lib/undo';
import { PhoneNote } from './PhoneNote';
import { SmtpPanel } from './SmtpPanel';

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
  'auth_recovery_lifetime',
  'auth_recovery_per_hour',
  'auth_second_factor_window',
  'auth_totp_required',
  'agent_version_retry',
  'agent_version_window',
  'agent_volume_interval',
  'agent_volume_stale',
  'agents_fingerprint_pairing',
  'audit_retention_days',
  'backup_interval',
  'backup_keep_auto',
  'backup_keep_pre_migration',
  'invite_lifetime',
  'login_lockout_duration',
  'login_max_client_failures',
  'login_max_failures',
  'login_max_tracked',
  'login_overflow_gap',
  'login_overflow_line',
  'profiles_poll_interval',
  'servers_connection_test_per_minute',
  'session_idle_timeout',
  'session_max_lifetime',
  'smtp_test_per_minute',
  'smtp_timeout',
  'supporters_enabled',
  'ui_shortcuts_enabled',
  'ws_max_sockets_per_session',
  'ws_max_subscriptions_per_socket',
];

// Sections of the settings (Design 5.6); keys not listed show under
// "other", so a new key is never hidden.
const sections: [string, RegExp][] = [
  ['sessions', /^session\./],
  ['login', /^login\./],
  ['invitations', /^invite\./],
  ['supporters', /^supporters\./],
  ['ratelimits', /^ratelimit\./],
  ['audit', /^audit\./],
  ['websocket', /^ws\./],
  ['profiles', /^profiles\./],
  ['agents', /^agents?\./],
  ['interface', /^ui\./],
  ['backups', /^backup\./],
  ['mail', /^smtp\./],
  ['twofactor', /^auth\./],
];
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

const sectionOf = (key: string) => sections.find(([, re]) => re.test(key))?.[0] ?? 'other';

// SettingsPage changes the global settings (Design 5.6): only the RCON
// admin. Changes wait for undo and go out with the settings' ETag
// (revision 2.4 candidate 24); switching supporters off asks first, as
// it ends the sessions of all supporters. A phone only reads (UI-012).
// SettingsPage holds the global settings and the SMTP setup (NFR-017).
export function SettingsPage() {
  return (
    <div className="flex flex-col gap-5">
      <SettingsForm />
      <SmtpPanel />
    </div>
  );
}

function SettingsForm() {
  const { t } = useTranslation();
  const phone = usePhone();
  const [showHelp, setShowHelp] = useState(storedHelp);
  const qc = useQueryClient();
  const { saved } = useUndo();
  const settings = useQuery({
    queryKey: settingsKey,
    queryFn: async () => {
      const r = await api.GET('/settings');
      return { values: must(r) as Values, etag: r.response.headers.get('ETag') ?? '' };
    },
  });
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
    const r = await outcome('settings', async () => {
      const res = await patchVersioned('/settings', etag, { body });
      must(res);
      next = res.response.headers.get('ETag') ?? '';
    });
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
  const groups = [...new Set(keys.map(sectionOf))];
  return (
    <form onSubmit={submit} className="flex max-w-3xl flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.settings')}</h1>
        <p className="text-sm text-muted">{t('settings.explain')}</p>
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
      </div>
      <PhoneNote />
      {settings.isPending && <p className="text-muted">{t('app.loading')}</p>}
      {groups.map((g) => (
        <section
          key={g}
          aria-label={t(`settings.section.${g}`)}
          className="rounded-lg border border-border bg-surface p-4"
        >
          <h2 className="mb-3 font-semibold">{t(`settings.section.${g}`)}</h2>
          <div className="grid gap-3 sm:grid-cols-2">
            {keys
              .filter((k) => sectionOf(k) === g)
              .map((k) => {
                const v =
                  base?.values[k] !== undefined && isGoDuration(base.values[k])
                    ? 'duration'
                    : typeof values[k];
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
                    <label
                      key={k}
                      className="flex items-center justify-between gap-3 text-sm sm:col-span-2"
                    >
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
                  v === 'duration'
                    ? (texts[k] ?? formatDuration(String(values[k])))
                    : String(values[k] ?? '');
                const bad =
                  v === 'duration' && texts[k] !== undefined && parseDuration(texts[k]) === null;
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
                    {bad && (
                      <span className="text-xs text-danger">{t('settings.badDuration')}</span>
                    )}
                  </div>
                );
              })}
          </div>
        </section>
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
