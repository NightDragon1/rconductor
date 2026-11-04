// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import { usePhone } from '@/lib/phone';

// PhoneNote says why a page of the administration offers no changes on a
// phone (UI-012). The backend checks every request the same.
export function PhoneNote({ users = false }: { users?: boolean }) {
  const { t } = useTranslation();
  if (!usePhone()) return null;
  return (
    <p className="rounded-md border border-border bg-surface-raised p-3 text-sm text-muted">
      {t(users ? 'admin.phoneReadOnlyUsers' : 'admin.phoneReadOnly')}
    </p>
  );
}
