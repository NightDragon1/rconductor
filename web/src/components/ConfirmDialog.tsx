// SPDX-License-Identifier: AGPL-3.0-or-later
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';

// ConfirmDialog asks before a destructive action (Design 6.1, K3).
export function ConfirmDialog({
  open,
  title,
  text,
  action,
  danger = true,
  onConfirm,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  text: string;
  action: string;
  danger?: boolean;
  onConfirm: () => void;
  onClose: () => void;
  children?: ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{text}</DialogDescription>
        {children}
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">{t('actions.cancel')}</Button>
          </DialogClose>
          <Button variant={danger ? 'danger' : 'default'} onClick={onConfirm}>
            {action}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
