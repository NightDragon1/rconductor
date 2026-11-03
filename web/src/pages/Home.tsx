// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import { useSession } from '@/api/session';

// HomePage stands in for the landing pages of step 3.
export function HomePage() {
  const { t } = useTranslation();
  const { data } = useSession();
  return (
    <>
      <h1 className="mb-2 text-2xl font-semibold">{t('home.title')}</h1>
      {data && (
        <p className="text-muted">
          {t('home.welcome', { name: data.user.pseudonym, role: t(`role.${data.user.role}`) })}
        </p>
      )}
    </>
  );
}
