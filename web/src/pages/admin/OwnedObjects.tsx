// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import { useSession } from '@/api/session';
import { useCommandLists, useUserGroups } from '@/api/users';

// OwnedObjects names the user groups and command lists that go with the
// users about to be deleted (D-106). It shows what the caller may read; the
// backend deletes them anyway.
export function OwnedObjects({ ids }: { ids: string[] }) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const role = session?.user.role;
  const admin = role === 'rcon_admin' || role === 'server_admin';
  const groups = (useUserGroups(admin).data ?? []).filter((g) => ids.includes(g.owner_id));
  const lists = (useCommandLists(admin).data ?? []).filter((l) => ids.includes(l.owner_id));
  if (groups.length === 0 && lists.length === 0) return null;
  return (
    <div className="grid gap-2 text-sm">
      <p>{t('admin.removeOwned')}</p>
      {groups.length > 0 && (
        <p>
          <span className="text-muted">{t('admin.removeGroups')}</span>{' '}
          {groups.map((g) => g.name).join(', ')}
        </p>
      )}
      {lists.length > 0 && (
        <p>
          <span className="text-muted">{t('admin.removeLists')}</span>{' '}
          {lists.map((l) => l.name).join(', ')}
        </p>
      )}
    </div>
  );
}
