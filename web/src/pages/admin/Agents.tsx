// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must, patchVersioned } from '@/api/client';
import { agentsKey, serversKey, useAgents, type Agent } from '@/api/servers';
import { outcome } from '@/api/users';
import type { components } from '@/api/schema';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { cell, headCell, rowButton } from '@/components/DenseTable';
import { CopyButton } from '@/components/CopyButton';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { reasonText } from '@/lib/admin';
import { ago, useNow } from '@/lib/format';
import { usePhone } from '@/lib/phone';
import { cn } from '@/lib/utils';
import { Card, Cards } from './Cards';
import { PhoneNote } from './PhoneNote';
import { ForeignText } from '@/components/ForeignText';
import { DockerHost } from '@/components/DockerHost';
import { PageHeader } from '@/components/PageHeader';

type Token = components['schemas']['AgentToken'];
const settingsKey = ['settings'] as const;

// PairDialog makes a pairing token (way A, AGT-005): token and a ready
// Compose service, shown only once.
function PairDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [token, setToken] = useState<Token | null>(null);
  const [error, setError] = useState('');
  const compose = useRef<HTMLPreElement>(null);
  const create = async () => {
    const r = await outcome('token', async () => {
      setToken(must(await api.POST('/agents/tokens')));
    });
    if (!r.ok) setError(reasonText(t, r));
    void qc.invalidateQueries({ queryKey: agentsKey });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogTitle>{t('agents.pairTitle')}</DialogTitle>
        <DialogDescription>{t('agents.pairExplain')}</DialogDescription>
        {token ? (
          <div className="flex flex-col gap-2">
            <p className="text-sm">
              {t('agents.fingerprint')}:{' '}
              <span className="font-mono">{token.panel_fingerprint}</span>
            </p>
            <pre
              ref={compose}
              aria-label={t('agents.compose')}
              className="max-h-72 overflow-auto rounded bg-surface-raised p-3 font-mono text-xs whitespace-pre"
            >
              {token.compose}
            </pre>
            <p className="text-sm text-warn">{t('agents.once')}</p>
            <CopyButton text={token.compose} target={compose} label={t('agents.copyCompose')} />
          </div>
        ) : (
          error && (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          )
        )}
        <div className="flex justify-end gap-2">
          <DialogClose asChild>
            <Button variant="ghost">{t('admin.close')}</Button>
          </DialogClose>
          {!token && <Button onClick={() => void create()}>{t('agents.makeToken')}</Button>}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// NameDialog sets the display name of an agent's host (UI-021, D-089);
// empty removes it. The backend checks role, length and control
// characters.
function NameDialog({ agent, onClose }: { agent: Agent; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [value, setValue] = useState(agent.display_name ?? '');
  const [error, setError] = useState('');
  const save = async () => {
    const r = await outcome('display-name', async () => {
      must(
        await api.PATCH('/agents/{id}', {
          params: { path: { id: agent.id } },
          body: { display_name: value.trim() === '' ? null : value },
        }),
      );
    });
    if (!r.ok) {
      setError(reasonText(t, r));
      return;
    }
    void qc.invalidateQueries({ queryKey: agentsKey });
    void qc.invalidateQueries({ queryKey: serversKey });
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogTitle>{t('agents.displayNameTitle')}</DialogTitle>
        <DialogDescription>{t('agents.displayNameExplain')}</DialogDescription>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <Input
            aria-label={t('agents.displayName')}
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
          {error && (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                {t('admin.close')}
              </Button>
            </DialogClose>
            <Button type="submit">{t('agents.displayNameSave')}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// AgentsPage lists the paired agents (AGT-005): pairing by token, approval
// of a waiting pairing by fingerprint, revocation. Only the RCON admin
// reaches it (Design 6.2); the backend decides. A phone only reads
// (UI-012).
export function AgentsPage() {
  const { t } = useTranslation();
  const [naming, setNaming] = useState<Agent | null>(null);
  const phone = usePhone();
  const qc = useQueryClient();
  const now = useNow(10_000);
  const agents = useAgents(true);
  const [q, setQ] = useState('');
  const [pairing, setPairing] = useState(false);
  const [revoke, setRevoke] = useState<Agent | null>(null);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  // Pairing by fingerprint is a setting (agents.fingerprint_pairing); it
  // switches itself off after 30 minutes (Design 1.2).
  const settings = useQuery({
    queryKey: settingsKey,
    queryFn: async () => {
      const r = await api.GET('/settings');
      return { values: must(r), etag: r.response.headers.get('ETag') ?? '' };
    },
  });
  const fingerprintOn = settings.data?.values['agents.fingerprint_pairing'] === true;

  const act = async (id: string, run: () => Promise<unknown>, done: string) => {
    const r = await outcome(id, run);
    setMessage(r.ok ? { ok: true, text: done } : { ok: false, text: reasonText(t, r) });
    void qc.invalidateQueries({ queryKey: agentsKey });
  };
  const toggleFingerprint = () =>
    act(
      'settings',
      async () => {
        must(
          await patchVersioned('/settings', settings.data?.etag ?? '', {
            body: { 'agents.fingerprint_pairing': !fingerprintOn },
          }),
        );
        await qc.invalidateQueries({ queryKey: settingsKey });
      },
      t('admin.saved'),
    );

  const all = agents.data ?? [];
  const needle = q.trim().toLowerCase();
  const shown = all.filter((a) => !needle || a.name.toLowerCase().includes(needle));
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t('nav.agents')}
        description={t('agents.explain')}
        actions={!phone && <Button onClick={() => setPairing(true)}>{t('agents.pair')}</Button>}
      >
        <p className="text-sm text-muted">
          {t('agents.summary', {
            count: all.filter((a) => a.status === 'active').length,
            connected: all.filter((a) => a.connected).length,
          })}
        </p>
      </PageHeader>
      <PhoneNote />
      <Input
        type="search"
        className="w-full sm:w-72"
        placeholder={t('agents.search')}
        aria-label={t('agents.searchLabel')}
        value={q}
        onChange={(e) => setQ(e.target.value)}
      />
      <label className="flex items-center justify-between gap-3 rounded-md border border-border bg-surface p-3 text-sm">
        <span>
          <span className="block font-medium">{t('agents.fingerprintPairing')}</span>
          <span className="text-xs text-muted">{t('agents.fingerprintHint')}</span>
        </span>
        <input
          type="checkbox"
          className="size-5"
          checked={fingerprintOn}
          disabled={phone || !settings.data}
          onChange={() => void toggleFingerprint()}
        />
      </label>
      {message && (
        <p
          role={message.ok ? 'status' : 'alert'}
          className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
        >
          {message.text}
        </p>
      )}
      {phone ? (
        <Cards
          label={t('nav.agents')}
          empty={all.length === 0 ? t(agents.isPending ? 'app.loading' : 'agents.none') : undefined}
        >
          {shown.map((a) => (
            <Card
              key={a.id}
              mono
              title={<ForeignText text={a.name} />}
              facts={[
                {
                  label: t('admin.statusLabel'),
                  value:
                    a.status === 'active'
                      ? t(a.connected ? 'agents.connected' : 'agents.disconnected')
                      : t(`agents.status.${a.status}`),
                },
                { label: t('agents.version'), value: <bdi className="font-mono">{a.version}</bdi> },
                { label: t('agents.dockerHost'), value: <DockerHost agent={a} /> },
                {
                  label: t('agents.displayName'),
                  value: a.display_name ? <ForeignText text={a.display_name} /> : '',
                },
                {
                  label: t('agents.fingerprint'),
                  value: <span className="font-mono text-xs">{a.fingerprint}</span>,
                },
                {
                  label: t('agents.lastSeen'),
                  value: a.last_seen_at ? ago(t, a.last_seen_at, now) : '',
                },
              ]}
            />
          ))}
        </Cards>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border bg-surface">
          <table className="w-full text-sm">
            <thead className="text-left text-muted">
              <tr>
                <th className={headCell}>{t('agents.name')}</th>
                <th className={headCell}>{t('admin.statusLabel')}</th>
                <th className={headCell}>{t('agents.version')}</th>
                <th className={headCell}>{t('agents.dockerHost')}</th>
                <th className={headCell}>{t('agents.displayName')}</th>
                <th className={headCell}>{t('agents.fingerprint')}</th>
                <th className={headCell}>{t('agents.lastSeen')}</th>
                <th className={cell} />
              </tr>
            </thead>
            <tbody>
              {shown.map((a) => (
                <tr key={a.id} id={a.id} className="border-t border-border">
                  <td className={`${cell} font-mono`}>
                    <ForeignText text={a.name} />
                  </td>
                  <td
                    className={cn(
                      cell,
                      a.status === 'pending'
                        ? 'text-warn'
                        : a.connected
                          ? 'text-ok'
                          : a.status === 'active'
                            ? 'text-danger'
                            : 'text-muted',
                    )}
                  >
                    {a.status === 'active'
                      ? t(a.connected ? 'agents.connected' : 'agents.disconnected')
                      : t(`agents.status.${a.status}`)}
                  </td>
                  <td className={`${cell} font-mono`}>
                    <bdi>{a.version}</bdi>
                  </td>
                  <td className={cell}>
                    <DockerHost agent={a} />
                  </td>
                  <td className={cell}>
                    {a.display_name && <ForeignText text={a.display_name} />}
                  </td>
                  <td
                    className={`max-w-56 wrap-anywhere md:truncate ${cell} font-mono text-xs`}
                    title={a.fingerprint}
                  >
                    {a.fingerprint}
                  </td>
                  <td className={`${cell} text-muted`}>
                    {a.last_seen_at ? ago(t, a.last_seen_at, now) : ''}
                  </td>
                  <td className={`${cell} text-right`}>
                    {a.status !== 'revoked' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className={rowButton}
                        onClick={() => setNaming(a)}
                      >
                        {t('agents.displayNameEdit')}
                      </Button>
                    )}
                    {a.status === 'pending' && (
                      <Button
                        size="sm"
                        className={rowButton}
                        onClick={() =>
                          void act(
                            a.id,
                            async () =>
                              must(
                                await api.POST('/agents/{id}/approve', {
                                  params: { path: { id: a.id } },
                                }),
                              ),
                            t('agents.approved', { name: a.name }),
                          )
                        }
                      >
                        {t('agents.approve')}
                      </Button>
                    )}
                    {a.status !== 'revoked' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className={cn('ml-2 text-danger', rowButton)}
                        onClick={() => setRevoke(a)}
                      >
                        {t(a.status === 'pending' ? 'agents.reject' : 'agents.revoke')}
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
              {all.length === 0 && (
                <tr>
                  <td colSpan={8} className="p-6 text-center text-muted">
                    {t(agents.isPending ? 'app.loading' : 'agents.none')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
      {pairing && <PairDialog onClose={() => setPairing(false)} />}
      {naming && <NameDialog agent={naming} onClose={() => setNaming(null)} />}
      <ConfirmDialog
        open={!!revoke}
        title={t('agents.revokeTitle', { name: revoke?.name ?? '' })}
        text={t('agents.revokeText')}
        action={t(revoke?.status === 'pending' ? 'agents.reject' : 'agents.revoke')}
        onClose={() => setRevoke(null)}
        onConfirm={() => {
          const a = revoke;
          setRevoke(null);
          if (a)
            void act(
              a.id,
              async () =>
                must(await api.DELETE('/agents/{id}', { params: { path: { id: a.id } } })),
              t('agents.revoked', { name: a.name }),
            );
        }}
      />
    </div>
  );
}
