// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import { invitesKey, rolesBelow, type Role, type User } from '@/api/users';
import { CopyButton } from '@/components/CopyButton';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { reasonText } from './BulkResults';

// InviteDialog creates an invitation link (UI-008, D-046), from a template
// user if one is chosen: the new user gets his rights and assignments.
// The link holds the token in its fragment and is shown only once
// (revision 2.4 candidate 23).
export function InviteDialog({
  open,
  callerRole,
  users,
  template,
  onClose,
}: {
  open: boolean;
  callerRole?: string;
  users: User[];
  template?: User;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const roles = rolesBelow(callerRole);
  const [role, setRole] = useState<Role | ''>(template?.role ?? roles[0] ?? '');
  const [from, setFrom] = useState(template?.id ?? '');
  const [url, setUrl] = useState('');
  const [error, setError] = useState('');
  const link = useRef<HTMLParagraphElement>(null);
  const roleId = useId();
  const fromId = useId();
  const candidates = users.filter((u) => u.role === role && !u.disabled);

  const create = async () => {
    setError('');
    try {
      const r = must(
        await api.POST('/invites', {
          body: { role: role as Role, template_user_id: from || undefined },
        }),
      );
      setUrl(r.url);
      void qc.invalidateQueries({ queryKey: invitesKey });
    } catch (e) {
      const reason = (e as { problem?: { reason?: string } }).problem?.reason;
      setError(reasonText(t, reason));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>{t(from ? 'admin.invite.fromTemplate' : 'admin.invite.title')}</DialogTitle>
        <DialogDescription>{t('admin.invite.explain')}</DialogDescription>
        {url ? (
          <div className="flex flex-col gap-2">
            <p ref={link} className="rounded bg-surface-raised p-2 font-mono text-xs break-all">
              {url}
            </p>
            <p className="text-sm text-warn">{t('admin.invite.once')}</p>
            <CopyButton text={url} target={link} label={t('admin.invite.copy')} />
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <label htmlFor={roleId} className="text-sm text-muted">
              {t('admin.role')}
            </label>
            <select
              id={roleId}
              className="h-11 rounded-md border border-border bg-surface px-3"
              value={role}
              onChange={(e) => {
                setRole(e.target.value as Role);
                setFrom('');
              }}
            >
              {roles.map((r) => (
                <option key={r} value={r}>
                  {t(`role.${r}`)}
                </option>
              ))}
            </select>
            <label htmlFor={fromId} className="text-sm text-muted">
              {t('admin.invite.template')}
            </label>
            <select
              id={fromId}
              className="h-11 rounded-md border border-border bg-surface px-3"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            >
              <option value="">{t('admin.invite.noTemplate')}</option>
              {candidates.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.pseudonym}
                </option>
              ))}
            </select>
            {error && (
              <p role="alert" className="text-sm text-danger">
                {error}
              </p>
            )}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">{t('admin.close')}</Button>
          </DialogClose>
          {!url && (
            <Button disabled={!role} onClick={() => void create()}>
              {t('admin.invite.create')}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
