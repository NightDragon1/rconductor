// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import { ServerBoard } from '@/components/ServerBoard';

// ServersPage lists the servers in scope as cards with a search (F13).
export function ServersPage() {
  const { t } = useTranslation();
  return <ServerBoard title={t('nav.servers')} />;
}
