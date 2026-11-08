// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
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
  // code is set once the password was right and the second factor follows
  // (AUTH-008).
  const [code, setCode] = useState(false);

  async function done(
    data: { must_change_password: boolean; totp_setup_required: boolean } | undefined,
  ) {
    qc.setQueryData(sessionKey, data);
    await navigate({
      to: data?.must_change_password
        ? '/change-password'
        : data?.totp_setup_required
          ? '/setup-second-factor'
          : '/',
    });
  }

  async function submitCode(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setError('');
    const r = await api.POST('/auth/login/totp', {
      body: { code: String(new FormData(e.currentTarget).get('code')) },
    });
    setBusy(false);
    if (!r.response.ok) {
      const s = r.response.status;
      if (s === 429 || (s === 401 && r.error?.detail !== 'code wrong')) setCode(false);
      setError(
        t(
          s === 429
            ? 'login.locked'
            : s === 401 && r.error?.detail !== 'code wrong'
              ? 'login.again'
              : 'login.codeFailed',
        ),
      );
      return;
    }
    await done(r.data);
  }

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
    if (r.response.status === 202) {
      setCode(true);
      return;
    }
    await done(r.data as { must_change_password: boolean; totp_setup_required: boolean });
  }

  if (code) {
    return (
      <main className="flex min-h-full items-center justify-center p-4">
        <Card className="w-full max-w-sm">
          <h1 className="mb-2 text-xl font-semibold">{t('login.codeTitle')}</h1>
          <p className="mb-6 text-sm text-muted">{t('login.codeExplain')}</p>
          <form className="space-y-4" onSubmit={(e) => void submitCode(e)}>
            <div className="space-y-1">
              <Label htmlFor="code">{t('twofactor.code')}</Label>
              <Input
                id="code"
                name="code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={16}
                autoFocus
                required
              />
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
          <Link to="/recover" className="tap mt-4 inline-block text-sm underline">
            {t('login.lostFactor')}
          </Link>
        </Card>
      </main>
    );
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
