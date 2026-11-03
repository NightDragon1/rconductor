// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import { sessionKey } from '@/api/session';
import { outcome } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { useUndo } from '@/lib/undo';

type Values = Record<string, unknown>;
export const settingsKey = ['settings'] as const;

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
  ['agents', /^agents\./],
  ['interface', /^ui\./],
  ['backups', /^backup\./],
];
const sectionOf = (key: string) => sections.find(([, re]) => re.test(key))?.[0] ?? 'other';

// SettingsPage changes the global settings (Design 5.6): only the RCON
// admin. Changes wait for undo and go out with the settings' ETag
// (revision 2.4 candidate 24); switching supporters off asks first, as
// it ends the sessions of all supporters.
export function SettingsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { schedule } = useUndo();
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
  const base = settings.data;
  const values = draft ?? base?.values ?? {};
  const changed = Object.keys(values).filter(
    (k) => base && JSON.stringify(values[k]) !== JSON.stringify(base.values[k]),
  );

  const send = () => {
    if (!base) return;
    const body = Object.fromEntries(changed.map((k) => [k, values[k]]));
    const etag = base.etag;
    schedule(
      t('settings.undo', { count: changed.length }),
      async (keepalive) => {
        const r = await outcome('settings', async () =>
          must(
            await api.PATCH('/settings', {
              params: { header: { 'If-Match': etag } },
              body,
              keepalive,
            }),
          ),
        );
        if (r.reason === 'conflict') {
          setConflict(true);
          setMessage({ ok: false, text: t('admin.conflict') });
          return;
        }
        setMessage(
          r.ok
            ? { ok: true, text: t('admin.saved') }
            : { ok: false, text: reasonText(t, r.reason) },
        );
        if (r.ok) setDraft(null);
        void qc.invalidateQueries({ queryKey: settingsKey });
        // ui.* and supporters.* reach the session (shortcuts, navigation).
        void qc.invalidateQueries({ queryKey: sessionKey });
      },
      { key: 'settings', onUndo: () => setDraft(null) },
    );
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (changed.includes('supporters.enabled') && values['supporters.enabled'] === false)
      setConfirmOff(true);
    else send();
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
      </div>
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
                const v = values[k];
                const id = `setting-${k}`;
                return typeof v === 'boolean' ? (
                  <label
                    key={k}
                    className="flex items-center justify-between gap-3 text-sm sm:col-span-2"
                  >
                    <span className="font-mono">{k}</span>
                    <input
                      id={id}
                      type="checkbox"
                      className="size-5"
                      disabled={conflict}
                      checked={v}
                      onChange={(e) => set(k, e.target.checked)}
                    />
                  </label>
                ) : (
                  <div key={k} className="flex flex-col gap-1">
                    <label htmlFor={id} className="font-mono text-sm text-muted">
                      {k}
                    </label>
                    <Input
                      id={id}
                      type={typeof v === 'number' ? 'number' : 'text'}
                      disabled={conflict}
                      value={String(v ?? '')}
                      onChange={(e) =>
                        set(k, typeof v === 'number' ? Number(e.target.value) : e.target.value)
                      }
                    />
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
        {!conflict && (
          <Button type="submit" disabled={changed.length === 0}>
            {t('settings.save', { count: changed.length })}
          </Button>
        )}
        {conflict && (
          <Button
            type="button"
            variant="outline"
            onClick={async () => {
              await qc.invalidateQueries({ queryKey: settingsKey });
              setDraft(null);
              setConflict(false);
              setMessage(null);
            }}
          >
            {t('admin.reload')}
          </Button>
        )}
        {changed.length > 0 && !conflict && (
          <Button type="button" variant="ghost" onClick={() => setDraft(null)}>
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
          send();
        }}
      />
    </form>
  );
}
