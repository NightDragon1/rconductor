// SPDX-License-Identifier: AGPL-3.0-or-later
import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import type { Server } from '@/api/servers';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { actionMode, actionState } from '@/lib/serverpage';

type Action = 'start' | 'stop' | 'restart';
const known = [
  'agent_unavailable',
  'action_failed',
  'rate_limited',
  'timeout',
  'server_unavailable',
  'forbidden',
];

// ServerActions are the buttons in the head of the server page: start,
// restart and stop with an agent, the restart over the stop command
// without one (AGT-008, AGT-009, revision 2.4 candidate 16). Moderator and
// supporter get none; the backend checks every action anyway. Stop and
// restart are destructive and ask first (Design 6.1).
export function ServerActions({ server, role }: { server: Server; role?: string }) {
  const { t } = useTranslation();
  const mode = actionMode(server, role);
  const state = actionState(server, mode);
  const [confirm, setConfirm] = useState<Action | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const act = useMutation({
    mutationFn: async (action: Action) =>
      must(
        await api.POST('/servers/{id}/actions', {
          params: { path: { id: server.id } },
          body: { action },
        }),
      ),
    onSuccess: (r) =>
      setMessage({
        ok: true,
        text: t(r.via === 'rcon' ? 'actions.done.rcon' : `actions.done.${r.action}`),
      }),
    onError: (e) => {
      const reason = e instanceof ApiError ? (e.problem?.reason ?? '') : '';
      setMessage({
        ok: false,
        text: t(known.includes(reason) ? `actions.error.${reason}` : 'actions.error.other'),
      });
    },
  });
  if (mode === 'none') return null;

  const run = (a: Action) => {
    setConfirm(null);
    setMessage(null);
    act.mutate(a);
  };
  const busy = act.isPending;
  const dialogKey =
    mode === 'rcon' ? 'rcon' : confirm === 'stop' ? 'stop' : confirm === 'restart' ? 'restart' : '';

  return (
    <div className="flex flex-col items-end gap-2">
      <div role="group" aria-label={t('actions.label')} className="flex flex-wrap gap-2">
        {mode === 'agent' ? (
          <>
            <Button variant="outline" disabled={!state.start || busy} onClick={() => run('start')}>
              {t('actions.start')}
            </Button>
            <Button
              variant="outline"
              disabled={!state.restart || busy}
              onClick={() => setConfirm('restart')}
            >
              {t('actions.restart')}
            </Button>
            <Button
              variant="outline"
              className="border-danger text-danger"
              disabled={!state.stop || busy}
              onClick={() => setConfirm('stop')}
            >
              {t('actions.stop')}
            </Button>
          </>
        ) : (
          <Button
            variant="outline"
            disabled={!state.restart || busy}
            onClick={() => setConfirm('restart')}
          >
            {t('actions.restartRcon')}
          </Button>
        )}
      </div>
      {state.agentGone && <p className="text-sm text-muted">{t('actions.agentGone')}</p>}
      <p role="status" className={message?.ok ? 'text-sm text-ok' : 'text-sm text-danger'}>
        {message?.text}
      </p>
      <Dialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
        <DialogContent>
          <DialogTitle>
            {dialogKey && t(`actions.confirm.${dialogKey}Title`, { name: server.name })}
          </DialogTitle>
          <DialogDescription>{dialogKey && t(`actions.confirm.${dialogKey}`)}</DialogDescription>
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button variant="ghost">{t('actions.cancel')}</Button>
            </DialogClose>
            <Button
              variant={confirm === 'stop' ? 'danger' : 'default'}
              onClick={() => confirm && run(confirm)}
            >
              {mode === 'rcon'
                ? t('actions.restartRcon')
                : confirm === 'stop'
                  ? t('actions.stop')
                  : t('actions.restart')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
