// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must, putVersioned } from '@/api/client';
import type { components } from '@/api/schema';
import { PasswordInput } from '@/components/PasswordInput';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { usePhone } from '@/lib/phone';

type SMTP = components['schemas']['SMTPSettings'];
type Encryption = SMTP['encryption'];
const smtpKey = ['settings', 'smtp'] as const;

interface Draft {
  host: string;
  port: string;
  encryption: Encryption;
  username: string;
  password: string;
  from: string;
}

const draftOf = (s: SMTP): Draft => ({
  host: s.host,
  port: String(s.port),
  encryption: s.encryption,
  username: s.username,
  password: '',
  from: s.from,
});

// SmtpPanel sets up the SMTP server of the panel and sends a test mail
// (NFR-017). The password is write-only: the panel only says whether one
// is set. User and password need STARTTLS or SSL; the backend checks
// everything again. Read-only on a phone (UI-012).
export function SmtpPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const readOnly = usePhone();
  const smtp = useQuery({
    queryKey: smtpKey,
    queryFn: async () => must(await api.GET('/settings/smtp')),
  });
  const [draft, setDraft] = useState<Draft | null>(null);
  const [removePassword, setRemovePassword] = useState(false);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [testOpen, setTestOpen] = useState(false);
  if (!smtp.data) {
    return smtp.isPending ? null : <p className="text-danger">{t('app.error')}</p>;
  }
  const d = draft ?? draftOf(smtp.data);
  const set = (patch: Partial<Draft>) => {
    setDraft({ ...d, ...patch });
    setMessage(null);
  };
  const tls = d.encryption !== 'none';

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const body: components['schemas']['SMTPSettingsInput'] = {
      host: d.host.trim(),
      port: Number(d.port),
      encryption: d.encryption,
      username: tls ? d.username : '',
      from: d.from.trim(),
    };
    if (d.password !== '') body.password = d.password;
    else if (removePassword || !tls) body.password = '';
    const r = await putVersioned('/settings/smtp', smtp.data.version, body);
    if (r.error !== undefined || !r.response.ok) {
      const problem = r.error as { reason?: string; detail?: string } | undefined;
      setMessage({
        ok: false,
        text: problem?.reason === 'conflict' ? t('admin.conflict') : reasonText(t, problem),
      });
      if (problem?.reason === 'conflict') void qc.invalidateQueries({ queryKey: smtpKey });
      return;
    }
    setDraft(null);
    setRemovePassword(false);
    qc.setQueryData(smtpKey, r.data);
    setMessage({ ok: true, text: t('admin.saved') });
  };

  return (
    <section
      aria-label={t('smtp.title')}
      className="flex max-w-3xl flex-col gap-3 rounded-lg border border-border bg-surface p-4"
    >
      <h2 className="font-semibold">{t('smtp.title')}</h2>
      <p className="text-sm text-muted">{t('smtp.explain')}</p>
      <form onSubmit={(e) => void save(e)} className="grid gap-3 sm:grid-cols-2">
        <label className="flex flex-col gap-1 text-sm">
          {t('smtp.host')}
          <Input
            value={d.host}
            maxLength={253}
            disabled={readOnly}
            onChange={(e) => set({ host: e.target.value })}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('smtp.port')}
          <Input
            inputMode="numeric"
            value={d.port}
            disabled={readOnly}
            onChange={(e) => set({ port: e.target.value })}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('smtp.encryption')}
          <select
            className="min-h-9 rounded-md border border-control bg-surface px-2 max-md:min-h-11"
            value={d.encryption}
            disabled={readOnly}
            onChange={(e) => set({ encryption: e.target.value as Encryption })}
          >
            <option value="starttls">{t('smtp.mode.starttls')}</option>
            <option value="ssl">{t('smtp.mode.ssl')}</option>
            <option value="none">{t('smtp.mode.none')}</option>
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('smtp.from')}
          <Input
            type="email"
            value={d.from}
            maxLength={254}
            disabled={readOnly}
            onChange={(e) => set({ from: e.target.value })}
          />
        </label>
        {tls ? (
          <>
            <label className="flex flex-col gap-1 text-sm">
              {t('smtp.username')}
              <Input
                value={d.username}
                maxLength={256}
                autoComplete="off"
                disabled={readOnly}
                onChange={(e) => set({ username: e.target.value })}
              />
            </label>
            <div className="flex flex-col gap-1 text-sm">
              <label htmlFor="smtp-password">{t('smtp.password')}</label>
              <PasswordInput
                id="smtp-password"
                value={d.password}
                maxLength={512}
                autoComplete="new-password"
                placeholder={
                  smtp.data.password_set ? t('smtp.passwordSet') : t('smtp.passwordNone')
                }
                disabled={readOnly}
                onChange={(e) => set({ password: e.target.value })}
              />
              {smtp.data.password_set && !readOnly && (
                <label className="flex items-center gap-2 text-muted">
                  <input
                    type="checkbox"
                    checked={removePassword}
                    onChange={(e) => setRemovePassword(e.target.checked)}
                  />
                  {t('smtp.removePassword')}
                </label>
              )}
            </div>
          </>
        ) : (
          <p className="text-sm text-warn sm:col-span-2">{t('smtp.noTLS')}</p>
        )}
        {!readOnly && (
          <div className="flex flex-wrap gap-2 sm:col-span-2">
            <Button type="submit" disabled={draft === null && !removePassword}>
              {t('admin.save')}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={draft !== null || !smtp.data.host || !smtp.data.from}
              onClick={() => setTestOpen(true)}
            >
              {t('smtp.test')}
            </Button>
          </div>
        )}
      </form>
      <p role="status" className={message?.ok ? 'text-sm text-ok' : 'text-sm text-danger'}>
        {message?.text}
      </p>
      <TestDialog open={testOpen} onClose={() => setTestOpen(false)} />
    </section>
  );
}

// TestDialog sends the fixed test mail to a recipient typed in and shows
// the result; a failure names only its category (NFR-017).
function TestDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const [to, setTo] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const send = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setResult(null);
    try {
      const r = must(await api.POST('/settings/smtp/test', { body: { to: to.trim() } }));
      setResult(
        r.ok
          ? { ok: true, text: t('smtp.sent') }
          : { ok: false, text: t(`smtp.failed.${r.category ?? 'connect'}`) },
      );
    } catch (err) {
      const reason = err instanceof ApiError ? err.problem?.reason : undefined;
      setResult({
        ok: false,
        text:
          reason === 'rate_limited'
            ? t('smtp.rateLimited')
            : reasonText(t, err instanceof ApiError ? err.problem : undefined),
      });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>{t('smtp.testTitle')}</DialogTitle>
        <DialogDescription>{t('smtp.testText')}</DialogDescription>
        <form onSubmit={(e) => void send(e)} className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            {t('smtp.to')}
            <Input
              type="email"
              required
              maxLength={254}
              value={to}
              onChange={(e) => setTo(e.target.value)}
            />
          </label>
          <p role="status" className={result?.ok ? 'text-sm text-ok' : 'text-sm text-danger'}>
            {result?.text}
          </p>
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                {t('actions.cancel')}
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy || to.trim() === ''}>
              {t('smtp.send')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
