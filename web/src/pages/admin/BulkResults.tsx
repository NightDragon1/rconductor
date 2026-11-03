// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import type { Outcome, User } from '@/api/users';
import { reasonText } from '@/lib/admin';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';

// BulkResults lists the outcome of a mass action per user: a partial
// failure never reads as success.
export function BulkResults({
  title,
  results,
  users,
  onClose,
}: {
  title: string;
  results: Outcome[] | null;
  users: User[];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  if (!results) return null;
  const failed = results.filter((r) => !r.ok).length;
  const name = (id: string) => users.find((u) => u.id === id)?.pseudonym ?? id;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>
          {failed === 0
            ? t('admin.bulk.allDone')
            : t('admin.bulk.someFailed', { ok: results.length - failed, failed })}
        </DialogDescription>
        <ul aria-label={t('admin.bulk.results')} className="max-h-72 overflow-y-auto">
          {results.map((r) => (
            <li
              key={r.id}
              className="flex justify-between gap-3 border-t border-border py-2 text-sm"
            >
              <bdi className="font-mono">{name(r.id)}</bdi>
              <span className={r.ok ? 'text-ok' : 'text-danger'}>
                {r.ok ? t('admin.bulk.ok') : reasonText(t, r.reason)}
              </span>
            </li>
          ))}
        </ul>
        <div className="flex justify-end">
          <DialogClose asChild>
            <Button>{t('admin.close')}</Button>
          </DialogClose>
        </div>
      </DialogContent>
    </Dialog>
  );
}
