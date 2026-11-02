// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { Outlet, useNavigate } from '@tanstack/react-router';
import { LogOut, SunMoon } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { Button } from '@/components/ui/button';
import { applyTheme, storedTheme } from '@/lib/theme';

// Shell is the frame of every page with a session; navigation follows in
// step 2 of M4.
export function Shell() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [theme, setTheme] = useState(storedTheme);

  async function logout() {
    await api.POST('/auth/logout');
    qc.clear();
    await navigate({ to: '/login' });
  }

  return (
    <div className="flex min-h-full flex-col">
      <header className="flex items-center justify-between border-b border-border bg-surface px-4">
        <span className="font-mono font-semibold">{t('app.name')}</span>
        <div className="flex gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('nav.theme')}
            title={t('nav.theme')}
            onClick={() => {
              const next = theme === 'dark' ? 'light' : 'dark';
              applyTheme(next);
              setTheme(next);
            }}
          >
            <SunMoon aria-hidden />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('nav.logout')}
            title={t('nav.logout')}
            onClick={logout}
          >
            <LogOut aria-hidden />
          </Button>
        </div>
      </header>
      <div className="flex-1 p-4">
        <Outlet />
      </div>
    </div>
  );
}
