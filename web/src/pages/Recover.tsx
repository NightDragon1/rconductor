// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { PasswordInput } from '@/components/PasswordInput';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';

// tokenOf reads the recovery token from the URL fragment, which the
// browser never sends to a server (D-046).
function tokenOf(): string {
  return decodeURIComponent(window.location.hash.replace(/^#/, ''));
}

// RecoverPage asks for a recovery link of the second factor, or with the
// token of the link resets it with the password (AUTH-008). The request
// answers alike for everyone; redeeming signs nobody in.
export function RecoverPage() {
  const { t } = useTranslation();
  const ids = { name: useId(), pw: useId() };
  const [token] = useState(tokenOf);
  const [state, setState] = useState<'form' | 'sent' | 'reset'>('form');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const request = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    await api.POST('/auth/recovery', {
      body: { pseudonym: String(new FormData(e.currentTarget).get('pseudonym')) },
    });
    setBusy(false);
    setState('sent');
  };
  const redeem = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    const r = await api.POST('/auth/recovery/redeem', {
      body: { token, password: String(new FormData(e.currentTarget).get('password')) },
    });
    setBusy(false);
    if (!r.response.ok) {
      setError(t(r.response.status === 429 ? 'login.locked' : 'recover.failed'));
      return;
    }
    // The token must not stay in the address bar or the history.
    window.history.replaceState(null, '', window.location.pathname);
    setState('reset');
  };

  return (
    <main className="flex min-h-full items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <h1 className="mb-2 text-xl font-semibold">{t('recover.title')}</h1>
        {state === 'sent' && <p role="status">{t('recover.sent')}</p>}
        {state === 'reset' && <p role="status">{t('recover.done')}</p>}
        {state === 'form' && !token && (
          <form className="space-y-4" onSubmit={(e) => void request(e)}>
            <p className="text-sm text-muted">{t('recover.explain')}</p>
            <div className="space-y-1">
              <label htmlFor={ids.name} className="text-sm">
                {t('login.pseudonym')}
              </label>
              <Input
                id={ids.name}
                name="pseudonym"
                autoComplete="username"
                maxLength={256}
                required
              />
            </div>
            <Button type="submit" className="w-full" disabled={busy}>
              {t('recover.send')}
            </Button>
          </form>
        )}
        {state === 'form' && token && (
          <form className="space-y-4" onSubmit={(e) => void redeem(e)}>
            <p className="text-sm text-muted">{t('recover.redeemExplain')}</p>
            <div className="space-y-1">
              <label htmlFor={ids.pw} className="text-sm">
                {t('login.password')}
              </label>
              <PasswordInput id={ids.pw} name="password" autoComplete="current-password" required />
            </div>
            {error && (
              <p role="alert" className="text-sm text-danger">
                {error}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={busy}>
              {t('recover.reset')}
            </Button>
          </form>
        )}
        <Link to="/login" className="tap mt-4 inline-block text-sm underline">
          {t('recover.toLogin')}
        </Link>
      </Card>
    </main>
  );
}
