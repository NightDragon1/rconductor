// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { PasswordInput } from '@/components/PasswordInput';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// takeToken reads the token from the fragment once and removes it from
// the address bar: a fragment never reaches a server, and the token goes
// only in the body of the redemption (revision 2.4 candidate 23).
let taken: string | undefined;
function takeToken(): string {
  if (taken === undefined || window.location.hash) {
    taken = decodeURIComponent(window.location.hash.replace(/^#/, ''));
    if (taken) {
      window.history.replaceState(null, '', window.location.pathname + window.location.search);
    }
  }
  return taken;
}

// InvitePage redeems an invitation with an own pseudonym and password
// (D-046, UI-008).
export function InvitePage() {
  const { t } = useTranslation();
  const [token] = useState(takeToken);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const pw = String(f.get('password'));
    if (pw.length < 12 || pw.length > 128) return setError(t('password.length'));
    if (pw !== String(f.get('repeat'))) return setError(t('password.mismatch'));
    setBusy(true);
    setError('');
    const r = await api.POST('/invites/redeem', {
      body: { token, pseudonym: String(f.get('pseudonym')).trim(), password: pw },
    });
    setBusy(false);
    if (r.response.ok) return setDone(true);
    const s = r.response.status;
    setError(t(s === 404 ? 'invite.expired' : s === 503 ? 'login.busy' : 'invite.rejected'));
  }

  return (
    <main className="flex min-h-full items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <h1 className="mb-2 text-xl font-semibold">{t('invite.title')}</h1>
        {done ? (
          <>
            <p role="status" className="mb-4 text-sm">
              {t('invite.done')}
            </p>
            <Link to="/login" className="tap text-accent underline">
              {t('login.submit')}
            </Link>
          </>
        ) : !token ? (
          <p role="alert" className="text-sm text-danger">
            {t('invite.noToken')}
          </p>
        ) : (
          <>
            <p className="mb-6 text-sm text-muted">{t('invite.explain')}</p>
            <form className="space-y-4" onSubmit={submit}>
              <div className="space-y-1">
                <Label htmlFor="pseudonym">{t('login.pseudonym')}</Label>
                <Input
                  id="pseudonym"
                  name="pseudonym"
                  autoComplete="username"
                  required
                  maxLength={64}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="password">{t('login.password')}</Label>
                <PasswordInput id="password" name="password" autoComplete="new-password" required />
              </div>
              <div className="space-y-1">
                <Label htmlFor="repeat">{t('password.repeat')}</Label>
                <PasswordInput id="repeat" name="repeat" autoComplete="new-password" required />
              </div>
              {error && (
                <p role="alert" className="text-sm text-danger">
                  {error}
                </p>
              )}
              <Button type="submit" className="w-full" disabled={busy}>
                {t('invite.submit')}
              </Button>
            </form>
          </>
        )}
      </Card>
    </main>
  );
}
