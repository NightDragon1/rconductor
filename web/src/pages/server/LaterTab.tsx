// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';

// laterTab stands in for a tab that a later step of M4 builds: console
// (5), commands (6) and logs (7).
export function laterTab() {
  return function LaterTab() {
    const { t } = useTranslation();
    return <p className="text-muted">{t('app.later')}</p>;
  };
}
