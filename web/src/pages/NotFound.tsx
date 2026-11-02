// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <main className="p-8">
      <h1 className="mb-4 text-xl font-semibold">{t('notFound.title')}</h1>
      <Link to="/" className="text-accent underline">
        {t('notFound.back')}
      </Link>
    </main>
  );
}
