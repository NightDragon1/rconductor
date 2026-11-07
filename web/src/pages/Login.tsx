// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { sessionKey } from '@/api/session';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { PasswordInput } from '@/components/PasswordInput';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

export function LoginPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError('');
    const r = await api.POST('/auth/login', {
      body: { pseudonym: String(f.get('pseudonym')), password: String(f.get('password')) },
    });
    setBusy(false);
    if (!r.response.ok) {
      const s = r.response.status;
      setError(t(s === 429 ? 'login.locked' : s === 503 ? 'login.busy' : 'login.failed'));
      return;
    }
    qc.setQueryData(sessionKey, r.data);
    await navigate({ to: r.data?.must_change_password ? '/change-password' : '/' });
  }

  return (
    <main className="flex min-h-full items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <h1 className="mb-6 text-xl font-semibold">{t('login.title')}</h1>
        <form className="space-y-4" onSubmit={submit}>
          <div className="space-y-1">
            <Label htmlFor="pseudonym">{t('login.pseudonym')}</Label>
            <Input id="pseudonym" name="pseudonym" autoComplete="username" required />
          </div>
          <div className="space-y-1">
            <Label htmlFor="password">{t('login.password')}</Label>
            <PasswordInput id="password" name="password" autoComplete="current-password" required />
          </div>
          {error && (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          )}
          <Button type="submit" className="w-full" disabled={busy}>
            {t('login.submit')}
          </Button>
        </form>
      </Card>
    </main>
  );
}
