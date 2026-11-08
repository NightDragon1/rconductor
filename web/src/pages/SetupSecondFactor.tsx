// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { sessionKey } from '@/api/session';
import { TwoFactorSetup } from '@/components/TwoFactorSetup';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';

// SetupSecondFactorPage is the only page while the RCON admin requires the
// second factor and the user has none (AUTH-008); the backend refuses
// everything else anyway.
export function SetupSecondFactorPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const done = async () => {
    await qc.invalidateQueries({ queryKey: sessionKey });
    await navigate({ to: '/' });
  };
  const logout = async () => {
    await api.POST('/auth/logout');
    qc.setQueryData(sessionKey, null);
    await navigate({ to: '/login' });
  };
  return (
    <main className="flex min-h-full items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <h1 className="mb-2 text-xl font-semibold">{t('twofactor.requiredTitle')}</h1>
        <p className="mb-6 text-sm text-muted">{t('twofactor.requiredExplain')}</p>
        <TwoFactorSetup onDone={() => void done()} />
        <Button variant="ghost" className="mt-4" onClick={() => void logout()}>
          {t('nav.logout')}
        </Button>
      </Card>
    </main>
  );
}
