// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { sessionKey } from '@/api/session';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// ChangePasswordPage is the only page while a password change is forced
// (AUTH-010); it also serves the voluntary change.
export function ChangePasswordPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [error, setError] = useState('');

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const next = String(f.get('new'));
    if (next !== String(f.get('repeat'))) return setError(t('password.mismatch'));
    if ([...next].length < 12 || [...next].length > 128) return setError(t('password.length'));
    const r = await api.POST('/me/password', {
      body: { current_password: String(f.get('current')), new_password: next },
    });
    if (!r.response.ok) return setError(t('password.failed'));
    await qc.invalidateQueries({ queryKey: sessionKey });
    await navigate({ to: '/' });
  }

  return (
    <main className="flex min-h-full items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <h1 className="mb-2 text-xl font-semibold">{t('password.title')}</h1>
        <p className="mb-6 text-sm text-muted">{t('password.explain')}</p>
        <form className="space-y-4" onSubmit={submit}>
          {(['current', 'new', 'repeat'] as const).map((name) => (
            <div key={name} className="space-y-1">
              <Label htmlFor={name}>{t(`password.${name}`)}</Label>
              <Input
                id={name}
                name={name}
                type="password"
                required
                autoComplete={name === 'current' ? 'current-password' : 'new-password'}
              />
            </div>
          ))}
          {error && (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          )}
          <Button type="submit" className="w-full">
            {t('password.submit')}
          </Button>
        </form>
      </Card>
    </main>
  );
}
