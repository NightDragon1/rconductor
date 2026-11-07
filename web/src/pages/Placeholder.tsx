// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';

// placeholder stands in for a page that a later step of M4 builds.
export function placeholder(key: string) {
  return function PlaceholderPage() {
    const { t } = useTranslation();
    return (
      <>
        <h1 className="mb-2 text-2xl font-semibold">{t(`nav.${key}`)}</h1>
        <p className="text-muted">{t('app.later')}</p>
      </>
    );
  };
}
