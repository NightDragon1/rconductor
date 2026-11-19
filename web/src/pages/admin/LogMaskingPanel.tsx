// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import type { components } from '@/api/schema';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { usePhone } from '@/lib/phone';

type Masking = components['schemas']['LogMasking'];
const maskingKey = ['settings', 'log-masking'] as const;

interface Draft {
  enabled: boolean;
  maxTexts: string;
  maxTextChars: string;
  budget: string;
  texts: string;
  ignoreCase: boolean;
  removeTexts: boolean;
}

const draftOf = (m: Masking): Draft => ({
  enabled: m.enabled,
  maxTexts: String(m.max_texts),
  maxTextChars: String(m.max_text_chars),
  budget: String(m.budget_ms),
  texts: '',
  ignoreCase: false,
  removeTexts: false,
});

// LogMaskingPanel sets up the masking of container logs (UI-022, D-088 c):
// the switch, the fixed texts of the RCON admin and the limits. The texts
// are write-only: the panel only shows how many are stored. The backend
// checks everything again. Read-only on a phone (UI-012).
export function LogMaskingPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const readOnly = usePhone();
  const masking = useQuery({
    queryKey: maskingKey,
    queryFn: async () => must(await api.GET('/settings/log-masking')),
  });
  const [draft, setDraft] = useState<Draft | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  if (!masking.data) {
    return masking.isPending ? null : <p className="text-danger">{t('app.error')}</p>;
  }
  const d = draft ?? draftOf(masking.data);
  const set = (patch: Partial<Draft>) => {
    setDraft({ ...d, ...patch });
    setMessage(null);
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    // Limits are whole numbers; anything else is refused before sending,
    // never sent as 0 or null (UI-016).
    if (![d.maxTexts, d.maxTextChars, d.budget].every((v) => /^\d+$/.test(v.trim()))) {
      setMessage({ ok: false, text: t('logMasking.notNumber') });
      return;
    }
    const body: components['schemas']['LogMaskingInput'] = {
      enabled: d.enabled,
      max_texts: Number(d.maxTexts),
      max_text_chars: Number(d.maxTextChars),
      budget_ms: Number(d.budget),
    };
    const texts = d.texts.split('\n').filter((l) => l.trim() !== '');
    if (texts.length > 0) body.texts = texts.map((value) => ({ value, ignore_case: d.ignoreCase }));
    else if (d.removeTexts) body.texts = [];
    const r = await api.PUT('/settings/log-masking', {
      params: { header: { 'If-Match': masking.data.version } },
      body,
    });
    if (r.error !== undefined || !r.response.ok) {
      const problem = r.error as { reason?: string; detail?: string } | undefined;
      setMessage({
        ok: false,
        text: problem?.reason === 'conflict' ? t('admin.conflict') : reasonText(t, problem),
      });
      if (problem?.reason === 'conflict') void qc.invalidateQueries({ queryKey: maskingKey });
      return;
    }
    setDraft(null);
    qc.setQueryData(maskingKey, r.data);
    setMessage({ ok: true, text: t('admin.saved') });
  };

  const number = (label: string, value: string, key: keyof Draft) => (
    <label className="flex flex-col gap-1 text-sm">
      {label}
      <Input
        inputMode="numeric"
        value={value}
        disabled={readOnly}
        onChange={(e) => set({ [key]: e.target.value })}
      />
    </label>
  );

  return (
    <section
      aria-label={t('logMasking.title')}
      className="flex max-w-3xl flex-col gap-3 rounded-lg border border-border bg-surface p-4"
    >
      <h2 className="font-semibold">{t('logMasking.title')}</h2>
      <p className="text-sm text-muted">{t('logMasking.explain')}</p>
      <form onSubmit={(e) => void save(e)} className="grid gap-3 sm:grid-cols-3">
        <label className="flex items-center gap-2 text-sm sm:col-span-3">
          <input
            type="checkbox"
            checked={d.enabled}
            disabled={readOnly}
            onChange={(e) => set({ enabled: e.target.checked })}
          />
          {t('logMasking.enabled')}
        </label>
        {!d.enabled && <p className="text-sm text-warn sm:col-span-3">{t('logMasking.offWarn')}</p>}
        <p className="text-sm sm:col-span-3">
          {t('logMasking.textCount', { count: masking.data.text_count })}
        </p>
        <label className="flex flex-col gap-1 text-sm sm:col-span-3">
          {t('logMasking.texts')}
          <textarea
            className="min-h-20 rounded-md border border-control bg-surface px-2 py-1 font-mono"
            value={d.texts}
            autoComplete="off"
            spellCheck={false}
            disabled={readOnly}
            onChange={(e) => set({ texts: e.target.value })}
          />
          <span className="text-muted">{t('logMasking.textsHint')}</span>
        </label>
        <label className="flex items-center gap-2 text-sm sm:col-span-3">
          <input
            type="checkbox"
            checked={d.ignoreCase}
            disabled={readOnly}
            onChange={(e) => set({ ignoreCase: e.target.checked })}
          />
          {t('logMasking.ignoreCase')}
        </label>
        {masking.data.text_count > 0 && !readOnly && (
          <label className="flex items-center gap-2 text-sm text-muted sm:col-span-3">
            <input
              type="checkbox"
              checked={d.removeTexts}
              onChange={(e) => set({ removeTexts: e.target.checked })}
            />
            {t('logMasking.removeTexts')}
          </label>
        )}
        {number(t('logMasking.maxTexts'), d.maxTexts, 'maxTexts')}
        {number(t('logMasking.maxTextChars'), d.maxTextChars, 'maxTextChars')}
        {number(t('logMasking.budget'), d.budget, 'budget')}
        <p className="text-sm text-muted sm:col-span-3">{t('logMasking.budgetHint')}</p>
        {!readOnly && (
          <div className="sm:col-span-3">
            <Button type="submit" disabled={draft === null}>
              {t('admin.save')}
            </Button>
          </div>
        )}
      </form>
      <p role="status" className={message?.ok ? 'text-sm text-ok' : 'text-sm text-danger'}>
        {message?.text}
      </p>
    </section>
  );
}
