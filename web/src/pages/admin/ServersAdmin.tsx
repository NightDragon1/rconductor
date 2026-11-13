// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useId, useState, type FormEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { certNotice } from '@/lib/cert';
import { api, must, patchVersioned } from '@/api/client';
import {
  serverGroupsKey,
  serversKey,
  useAgents,
  useProfiles,
  useServerGroups,
  useServers,
  type Server,
  type ServerGroup,
} from '@/api/servers';
import { useSession } from '@/api/session';
import { outcome } from '@/api/users';
import type { components } from '@/api/schema';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { topProfile } from '@/lib/profiles';
import { stripCodes } from '@/lib/serverpage';
import { cardStatus } from '@/lib/status';
import { usePhone, useReveal } from '@/lib/phone';
import { useShortcuts } from '@/lib/shortcuts';
import { useUndo, useUnsaved } from '@/lib/undo';
import { cn } from '@/lib/utils';
import { Card, Cards } from './Cards';
import { PhoneNote } from './PhoneNote';
import { ProfileList } from './ProfileList';
import { ForeignText } from '@/components/ForeignText';

type GamePort = components['schemas']['GamePort'];
type Message = { ok: boolean; text: string } | null;

const field = 'h-11 rounded-md border border-control bg-surface px-3 disabled:opacity-70';

function Row({ label, children }: { label: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm text-muted">
        {label}
      </label>
      {children(id)}
    </div>
  );
}

function Note({ message }: { message: Message }) {
  if (!message) return null;
  return (
    <p
      role={message.ok ? 'status' : 'alert'}
      className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
    >
      {message.text}
    </p>
  );
}

interface Draft {
  name: string;
  // Ordered, top first (SRV-008, CAT-007).
  profile_ids: string[];
  host: string;
  port: string;
  password: string;
  agent_id: string;
  container_name: string;
  ownPorts: boolean;
  game_ports: GamePort[];
  // The certificate to trust (protocol http-api, PROT-003); set from the
  // connection test, sent with save.
  tls_fingerprint: string;
}

// Seen is the certificate the last connection test read.
interface Seen {
  fingerprint: string;
  notAfter?: string;
}

// groupFingerprint shows a SHA-256 in hex as pairs, easier to compare.
const groupFingerprint = (fp: string) => fp.match(/.{1,2}/g)?.join(':') ?? fp;

const draftOf = (s?: Server): Draft => ({
  name: s?.name ?? '',
  profile_ids: s?.profile_ids ?? [],
  host: s?.host ?? '',
  port: s?.port ? String(s.port) : '',
  password: '',
  agent_id: s?.agent_id ?? '',
  container_name: s?.container_name ?? '',
  ownPorts: s?.game_ports_override ?? false,
  game_ports: s?.game_ports ?? [],
  tls_fingerprint: s?.tls_fingerprint ?? '',
});

// ServerPanel creates or changes a server (SRV-001, SRV-005): only the
// RCON admin; a server admin may change only the profile list of a server
// in his scope (SRV-008), the backend checks it; a phone sees it read-only.
// The password is only set, never shown. Agent and container come from the agent's list
// (revision 2.4 candidate 22).
function ServerPanel({
  server,
  canEdit,
  canEditProfiles,
  canSeeAgents,
  onClose,
  onCreated,
}: {
  server?: Server;
  canEdit: boolean;
  canEditProfiles: boolean;
  canSeeAgents: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const { saved, saving } = useUndo();
  const profiles = useProfiles();
  const agents = useAgents(canSeeAgents);
  const panel = useReveal<HTMLElement>();
  const [d, setD] = useState<Draft>(() => draftOf(server));
  const [message, setMessage] = useState<Message>(null);
  const [conflict, setConflict] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [seen, setSeen] = useState<Seen | null>(null);
  const [base, setBase] = useState(server);
  useUnsaved(JSON.stringify(d) !== JSON.stringify(draftOf(base)));
  const containers = useQuery({
    queryKey: ['agent-containers', d.agent_id],
    queryFn: async () =>
      must(await api.GET('/agents/{id}/containers', { params: { path: { id: d.agent_id } } })),
    enabled: canSeeAgents && !!d.agent_id,
  });
  const set = (p: Partial<Draft>) => setD((x) => ({ ...x, ...p }));
  const profileIds = (x: Draft) => (x.profile_ids.length ? x.profile_ids : undefined);
  // A server admin sends the profile list alone (SRV-008).
  const bodyOf = (x: Draft) => (canEdit ? fullBody(x) : { profile_ids: profileIds(x) });
  const fullBody = (x: Draft) => ({
    name: x.name.trim(),
    profile_ids: profileIds(x),
    host: x.host.trim(),
    port: x.port ? Number(x.port) : undefined,
    password: x.password || undefined,
    agent_id: x.agent_id,
    container_name: x.agent_id ? x.container_name : '',
    game_ports: x.ownPorts ? x.game_ports : [],
    tls_fingerprint: x.tls_fingerprint || undefined,
  });
  const body = () => fullBody(d);
  // The protocol of the top profile decides the form (D-074).
  const httpApi =
    (profiles.data ?? []).find((p) => p.id === d.profile_ids[0])?.protocol === 'http-api';
  const lng = i18n.language;
  const day = (iso: string) => new Date(iso).toLocaleDateString(lng);

  // Tests what is in the form without saving it (SRV-007).
  const testConnection = async () => {
    setTesting(true);
    setMessage(null);
    setSeen(null);
    let result = '';
    const r = await outcome(server?.id ?? 'new', async () => {
      const { host, port, profile_ids, password, tls_fingerprint } = body();
      const res = must(
        await api.POST('/servers/test-connection', {
          body: { server_id: server?.id, host, port, profile_ids, password, tls_fingerprint },
        }),
      );
      result = res.result;
      if (res.fingerprint) setSeen({ fingerprint: res.fingerprint, notAfter: res.not_after });
    });
    setTesting(false);
    // A server with an HTTPS API checks a token, not a password.
    const key =
      httpApi && (result === 'ok' || result === 'auth_failed') ? `${result}_token` : result;
    setMessage(
      r.ok
        ? { ok: result === 'ok', text: t(`servers.testResult.${key}`) }
        : { ok: false, text: reasonText(t, r) },
    );
  };

  const save = (e: FormEvent) => {
    e.preventDefault();
    if (!server) {
      void (async () => {
        const r = await saving(
          outcome('new', async () => {
            must(await api.POST('/servers', { body: body() }));
            onCreated();
          }),
        );
        void qc.invalidateQueries({ queryKey: serversKey });
        if (!r.ok) setMessage({ ok: false, text: reasonText(t, r) });
      })();
      return;
    }
    void change(server);
  };
  // Save goes out at once; Undo sends the previous state, but never for a
  // new password: the old one is not known here (D-048). Host and port
  // keep the stored password, so they can be undone (D-050).
  const write = async (id: string, from: Server, next: Draft) => {
    let done = null as Server | null;
    const r = await saving(
      outcome(id, async () => {
        done = must(
          await patchVersioned('/servers/{id}', from.version, {
            path: { id },
            body: bodyOf(next),
          }),
        );
      }),
    );
    if (r.reason === 'conflict') {
      setConflict(true);
      setMessage({ ok: false, text: t('admin.conflict') });
      return null;
    }
    setMessage(r.ok ? { ok: true, text: t('admin.saved') } : { ok: false, text: reasonText(t, r) });
    if (done) {
      setBase(done);
      setD(draftOf(done));
    }
    void qc.invalidateQueries({ queryKey: serversKey });
    return done;
  };
  const change = async (s: Server) => {
    const before = base ?? s;
    const old = draftOf(before);
    const credentials = !!d.password;
    const after = await write(s.id, before, d);
    if (!after) return;
    const label = t('servers.undo.changed', { name: after.name });
    if (credentials) saved(label);
    else saved(label, () => void write(s.id, after, old));
  };

  const off = !canEdit || conflict;
  const mayProfiles = canEdit || (canEditProfiles && !!server);
  return (
    <aside
      ref={panel}
      aria-label={t('admin.panel', { name: server?.name ?? t('servers.new') })}
      className="flex w-full flex-col gap-3 rounded-lg border border-border bg-surface p-5 xl:w-[28rem]"
    >
      <div className="flex items-start justify-between gap-3">
        <h2 className="wrap-anywhere md:truncate text-lg font-semibold">
          <bdi>{server?.name ?? t('servers.new')}</bdi>
        </h2>
        <Button variant="ghost" size="sm" aria-label={t('admin.closePanel')} onClick={onClose}>
          <X aria-hidden className="size-4" />
        </Button>
      </div>
      {server?.credentials && server.credentials !== 'ok' && (
        <p className="text-sm text-warn">{t(`servers.credentials.${server.credentials}`)}</p>
      )}
      <form onSubmit={save} className="flex flex-col gap-3">
        <Row label={t('servers.name')}>
          {(id) => (
            <Input
              id={id}
              disabled={off}
              value={d.name}
              onChange={(e) => set({ name: e.target.value })}
              required
            />
          )}
        </Row>
        <ProfileList
          ids={d.profile_ids}
          profiles={profiles.data ?? []}
          disabled={!mayProfiles || conflict}
          onChange={(profile_ids) => {
            setMessage(null);
            set({ profile_ids });
          }}
        />
        {/* Host and RCON port come for the RCON admin only (D-056). */}
        {(canEdit || server?.host !== undefined) && (
          <div className="grid grid-cols-[1fr_7rem] gap-2">
            <Row label={t('servers.host')}>
              {(id) => (
                <Input
                  id={id}
                  disabled={off}
                  value={d.host}
                  onChange={(e) => set({ host: e.target.value })}
                  required
                />
              )}
            </Row>
            <Row label={httpApi ? t('servers.apiPort') : t('servers.port')}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  max={65535}
                  disabled={off}
                  value={d.port}
                  onChange={(e) => set({ port: e.target.value })}
                />
              )}
            </Row>
          </div>
        )}
        {canEdit && (
          <Row
            label={
              httpApi
                ? server
                  ? t('servers.newApiToken')
                  : t('servers.apiToken')
                : server
                  ? t('servers.newPassword')
                  : t('servers.password')
            }
          >
            {(id) => (
              <Input
                id={id}
                type="password"
                autoComplete="new-password"
                disabled={off}
                required={!server}
                placeholder={server ? t('servers.unchanged') : ''}
                value={d.password}
                onChange={(e) => set({ password: e.target.value })}
              />
            )}
          </Row>
        )}
        {httpApi && canEdit && <p className="text-xs text-muted">{t('servers.apiTokenHint')}</p>}
        {httpApi && (
          <CertificateBlock
            trusted={d.tls_fingerprint}
            stored={base?.tls_fingerprint}
            notAfter={base?.tls_fingerprint === d.tls_fingerprint ? base?.tls_not_after : undefined}
            seen={seen}
            canTrust={!off}
            onTrust={(fp) => set({ tls_fingerprint: fp })}
            day={day}
          />
        )}
        <Row label={t('servers.agent')}>
          {(id) => (
            <select
              id={id}
              className={field}
              disabled={off}
              value={d.agent_id}
              onChange={(e) => set({ agent_id: e.target.value, container_name: '' })}
            >
              <option value="">{t('servers.noAgent')}</option>
              {(agents.data ?? [])
                .filter((a) => a.status === 'active' || a.id === d.agent_id)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    <ForeignText text={a.name} />
                  </option>
                ))}
              {!canSeeAgents && d.agent_id && (
                <option value={d.agent_id}>{t('servers.agentSet')}</option>
              )}
            </select>
          )}
        </Row>
        {d.agent_id && (
          <Row label={t('servers.container')}>
            {(id) => (
              <select
                id={id}
                className={field}
                disabled={off}
                value={d.container_name}
                onChange={(e) => set({ container_name: e.target.value })}
                required
              >
                <option value="">{t('servers.choose')}</option>
                {(containers.data?.items ?? []).map((c) => (
                  <option key={c.name} value={c.name}>
                    {t('servers.containerOption', { name: stripCodes(c.name), state: c.state })}
                  </option>
                ))}
                {d.container_name &&
                  !(containers.data?.items ?? []).some((c) => c.name === d.container_name) && (
                    <option value={d.container_name}>{d.container_name}</option>
                  )}
              </select>
            )}
          </Row>
        )}
        {containers.data && !containers.data.connected && (
          <p className="text-sm text-warn">{t('servers.agentAway')}</p>
        )}
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            disabled={off}
            checked={d.ownPorts}
            onChange={(e) => set({ ownPorts: e.target.checked })}
          />
          {t('servers.ownPorts')}
        </label>
        {d.ownPorts && (
          <div className="flex flex-col gap-2">
            {d.game_ports.map((p, i) => (
              <div key={i} className="grid grid-cols-[6rem_5rem_1fr_auto] gap-2">
                <Input
                  type="number"
                  aria-label={t('servers.portNumber')}
                  disabled={off}
                  value={p.port}
                  onChange={(e) =>
                    set({
                      game_ports: d.game_ports.map((x, j) =>
                        j === i ? { ...x, port: Number(e.target.value) } : x,
                      ),
                    })
                  }
                />
                <select
                  aria-label={t('servers.proto')}
                  className={field}
                  disabled={off}
                  value={p.proto}
                  onChange={(e) =>
                    set({
                      game_ports: d.game_ports.map((x, j) =>
                        j === i ? { ...x, proto: e.target.value as GamePort['proto'] } : x,
                      ),
                    })
                  }
                >
                  <option value="tcp">tcp</option>
                  <option value="udp">udp</option>
                </select>
                <Input
                  aria-label={t('servers.portName')}
                  disabled={off}
                  value={p.name}
                  onChange={(e) =>
                    set({
                      game_ports: d.game_ports.map((x, j) =>
                        j === i ? { ...x, name: e.target.value } : x,
                      ),
                    })
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={off}
                  aria-label={t('admin.removeItem', { name: String(p.port) })}
                  onClick={() => set({ game_ports: d.game_ports.filter((_, j) => j !== i) })}
                >
                  <X aria-hidden className="size-4" />
                </Button>
              </div>
            ))}
            {!off && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="self-start"
                onClick={() =>
                  set({
                    game_ports: [...d.game_ports, { port: 25565, proto: 'tcp', name: 'game' }],
                  })
                }
              >
                {t('servers.addPort')}
              </Button>
            )}
          </div>
        )}
        <Note message={message} />
        {mayProfiles && (
          <div className="flex flex-wrap gap-2">
            {!conflict && (
              <Button type="submit">{server ? t('servers.save') : t('servers.create')}</Button>
            )}
            {canEdit && !conflict && (
              <Button
                type="button"
                variant="outline"
                disabled={testing || !d.host.trim()}
                onClick={() => void testConnection()}
              >
                {t('servers.testConnection')}
              </Button>
            )}
            {conflict && (
              <Button
                type="button"
                variant="outline"
                onClick={async () => {
                  const fresh = (
                    await qc.fetchQuery({
                      queryKey: serversKey,
                      staleTime: 0,
                      queryFn: async () => must(await api.GET('/servers')).items,
                    })
                  ).find((s) => s.id === server?.id);
                  setBase(fresh);
                  setD(draftOf(fresh));
                  setConflict(false);
                  setMessage(null);
                }}
              >
                {t('admin.reload')}
              </Button>
            )}
            {canEdit && server && (
              <Button
                type="button"
                variant="outline"
                className="border-danger text-danger"
                onClick={() => setRemoving(true)}
              >
                {t('servers.delete')}
              </Button>
            )}
          </div>
        )}
      </form>
      {server && (
        <ConfirmDialog
          open={removing}
          title={t('servers.deleteTitle', { name: server.name })}
          text={t('servers.deleteText')}
          action={t('servers.delete')}
          onClose={() => setRemoving(false)}
          onConfirm={async () => {
            setRemoving(false);
            const r = await outcome(server.id, async () =>
              must(await api.DELETE('/servers/{id}', { params: { path: { id: server.id } } })),
            );
            void qc.invalidateQueries({ queryKey: serversKey });
            if (r.ok) onClose();
            else setMessage({ ok: false, text: reasonText(t, r) });
          }}
        />
      )}
    </aside>
  );
}

// GroupPanel changes a server group (SRV-003): its servers, as a whole
// with If-Match; a server may be in several groups.
function GroupPanel({
  group,
  canEdit,
  servers,
  onClose,
}: {
  group: ServerGroup;
  canEdit: boolean;
  servers: Server[];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { saved, saving } = useUndo();
  const panel = useReveal<HTMLElement>();
  const [draft, setDraft] = useState(group);
  const [message, setMessage] = useState<Message>(null);
  const [conflict, setConflict] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [base, setBase] = useState(group);
  const dirty = JSON.stringify(draft.server_ids) !== JSON.stringify(base.server_ids);
  useUnsaved(dirty);
  const pickId = useId();
  const name = (id: string) => servers.find((s) => s.id === id)?.name ?? id;

  // Edits change the draft; Save sends it at once (D-048).
  const write = async (from: ServerGroup, to: ServerGroup) => {
    let done = null as ServerGroup | null;
    const r = await saving(
      outcome(group.id, async () => {
        done = must(
          await patchVersioned('/server-groups/{id}', from.version, {
            path: { id: group.id },
            body: { name: to.name, server_ids: to.server_ids },
          }),
        );
      }),
    );
    if (r.reason === 'conflict') {
      setConflict(true);
      setMessage({ ok: false, text: t('admin.conflict') });
      return null;
    }
    setMessage(r.ok ? { ok: true, text: t('admin.saved') } : { ok: false, text: reasonText(t, r) });
    if (done) {
      setBase(done);
      setDraft(done);
    }
    void qc.invalidateQueries({ queryKey: serverGroupsKey });
    void qc.invalidateQueries({ queryKey: serversKey });
    return done;
  };
  const save = async () => {
    const before = base;
    const after = await write(before, draft);
    if (after) {
      saved(t('servers.undo.groupChanged', { name: after.name }), () => void write(after, before));
    }
  };
  const change = (next: ServerGroup) => {
    if (conflict) return;
    setMessage(null);
    setDraft(next);
  };

  return (
    <aside
      ref={panel}
      aria-label={t('admin.panel', { name: draft.name })}
      className="flex w-full flex-col gap-3 rounded-lg border border-border bg-surface p-5 xl:w-[28rem]"
    >
      <div className="flex items-start justify-between gap-3">
        <h2 className="wrap-anywhere md:truncate font-mono text-lg font-semibold uppercase">
          <bdi>{draft.name}</bdi>
        </h2>
        <Button variant="ghost" size="sm" aria-label={t('admin.closePanel')} onClick={onClose}>
          <X aria-hidden className="size-4" />
        </Button>
      </div>
      <h3 className="text-sm text-muted">{t('servers.groupServers')}</h3>
      <ul aria-label={t('servers.groupServers')} className="flex flex-wrap gap-2">
        {draft.server_ids.map((id) => (
          <li
            key={id}
            className="flex items-center gap-1 rounded-md border border-border px-2 py-1 font-mono text-sm"
          >
            <bdi>{name(id)}</bdi>
            {canEdit && (
              <button
                type="button"
                className="ml-1 text-muted hover:text-danger"
                aria-label={t('admin.removeItem', { name: name(id) })}
                onClick={() =>
                  change({ ...draft, server_ids: draft.server_ids.filter((x) => x !== id) })
                }
              >
                <X aria-hidden className="size-4" />
              </button>
            )}
          </li>
        ))}
      </ul>
      {canEdit && (
        <>
          <label htmlFor={pickId} className="sr-only">
            {t('servers.addToGroup')}
          </label>
          <select
            id={pickId}
            className="h-9 rounded-md border border-control bg-surface px-2 text-sm"
            value=""
            onChange={(e) =>
              e.target.value &&
              change({ ...draft, server_ids: [...draft.server_ids, e.target.value] })
            }
          >
            <option value="">{t('servers.addToGroup')}</option>
            {servers
              .filter((s) => !draft.server_ids.includes(s.id))
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
          </select>
        </>
      )}
      <Note message={message} />
      {canEdit && conflict && (
        <Button
          variant="outline"
          className="self-start"
          onClick={async () => {
            const fresh = must(
              await api.GET('/server-groups/{id}', { params: { path: { id: group.id } } }),
            );
            setBase(fresh);
            setDraft(fresh);
            setConflict(false);
            setMessage(null);
          }}
        >
          {t('admin.reload')}
        </Button>
      )}
      {canEdit && !conflict && (
        <Button className="self-start" disabled={!dirty} onClick={() => void save()}>
          {t('admin.save')}
        </Button>
      )}
      {canEdit && (
        <Button
          variant="outline"
          className="self-start border-danger text-danger"
          onClick={() => setRemoving(true)}
        >
          {t('servers.deleteGroup')}
        </Button>
      )}
      <ConfirmDialog
        open={removing}
        title={t('servers.deleteGroupTitle', { name: group.name })}
        text={t('servers.deleteGroupText')}
        action={t('servers.deleteGroup')}
        onClose={() => setRemoving(false)}
        onConfirm={async () => {
          setRemoving(false);
          const r = await outcome(group.id, async () =>
            must(await api.DELETE('/server-groups/{id}', { params: { path: { id: group.id } } })),
          );
          void qc.invalidateQueries({ queryKey: serverGroupsKey });
          if (r.ok) onClose();
          else setMessage({ ok: false, text: reasonText(t, r) });
        }}
      />
    </aside>
  );
}

// ServersAdminPage lists servers and server groups (SRV-001, SRV-003):
// the RCON admin creates and changes them, a server admin reads those in
// his scope (Design 6.2); a phone only reads (UI-012).
export function ServersAdminPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: session } = useSession();
  const phone = usePhone();
  // Agents, host and RCON port: the RCON admin only (D-056).
  const canSeeAgents = session?.user.role === 'rcon_admin';
  const canEdit = canSeeAgents && !phone;
  // A server admin changes the profile list of the servers in his scope;
  // the list shows only those (SRV-008, Design 6.2).
  const canEditProfiles = session?.user.role === 'server_admin' && !phone;
  const servers = useServers();
  const groups = useServerGroups(true);
  const profiles = useProfiles();
  const [open, setOpen] = useState<{ kind: 'server' | 'group'; id: string } | null>(null);
  const { guard } = useUndo();
  const [groupName, setGroupName] = useState('');
  const [error, setError] = useState('');
  const [q, setQ] = useState('');
  const groupNameId = useId();
  useShortcuts({ Escape: () => guard(() => setOpen(null)) });
  const all = servers.data ?? [];
  const needle = q.trim().toLowerCase();
  const shown = all.filter(
    (s) =>
      !needle || s.name.toLowerCase().includes(needle) || s.host?.toLowerCase().includes(needle),
  );
  const shownGroups = (groups.data ?? []).filter(
    (g) => !needle || g.name.toLowerCase().includes(needle),
  );
  const profileName = (ids?: string[]) => topProfile(t, ids, profiles.data ?? []);
  const groupNames = (s: Server) =>
    (groups.data ?? [])
      .filter((g) => g.server_ids.includes(s.id))
      .map((g) => g.name)
      .join(', ');
  const current =
    open?.kind === 'server' && open.id !== 'new' ? all.find((s) => s.id === open.id) : undefined;
  const currentGroup =
    open?.kind === 'group' ? groups.data?.find((g) => g.id === open.id) : undefined;

  const createGroup = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    const r = await outcome('new', async () => {
      const g = must(
        await api.POST('/server-groups', { body: { name: groupName.trim(), server_ids: [] } }),
      );
      setGroupName('');
      setOpen({ kind: 'group', id: g.id });
    });
    void qc.invalidateQueries({ queryKey: serverGroupsKey });
    if (!r.ok) setError(reasonText(t, r));
  };

  return (
    <div className="flex flex-col gap-4 xl:flex-row xl:items-start">
      <div className="flex min-w-0 flex-1 flex-col gap-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold">{t('nav.serverAdmin')}</h1>
            <p className="text-sm text-muted">
              {t(
                canEdit
                  ? 'servers.explainAdmin'
                  : canEditProfiles
                    ? 'servers.explainProfiles'
                    : 'servers.explainRead',
              )}
            </p>
          </div>
          {canEdit && (
            <Button onClick={() => guard(() => setOpen({ kind: 'server', id: 'new' }))}>
              {t('servers.new')}
            </Button>
          )}
        </div>

        <PhoneNote />

        <Input
          type="search"
          className="w-full sm:w-72"
          placeholder={t('servers.search')}
          aria-label={t('servers.searchLabel')}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />

        {phone ? (
          <Cards label={t('nav.servers')}>
            {shown.map((s) => (
              <Card
                key={s.id}
                mono
                title={<bdi>{s.name}</bdi>}
                selected={open?.id === s.id}
                onOpen={() => guard(() => setOpen({ kind: 'server', id: s.id }))}
                facts={[
                  { label: t('admin.statusLabel'), value: t(`status.${cardStatus(s)}`) },
                  { label: t('servers.profile'), value: profileName(s.profile_ids) },
                ]}
              />
            ))}
          </Cards>
        ) : (
          <section
            aria-label={t('nav.servers')}
            className="overflow-x-auto rounded-lg border border-border bg-surface"
          >
            <table className="w-full text-sm">
              <thead className="text-left text-muted">
                <tr>
                  <th className="p-3 font-medium">{t('servers.name')}</th>
                  <th className="p-3 font-medium">{t('servers.profile')}</th>
                  {canSeeAgents && <th className="p-3 font-medium">{t('servers.address')}</th>}
                  <th className="p-3 font-medium">{t('servers.container')}</th>
                  <th className="p-3 font-medium">{t('servers.groups')}</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((s) => (
                  <tr
                    key={s.id}
                    className={cn(
                      'border-t border-border',
                      open?.id === s.id && 'bg-surface-raised',
                    )}
                  >
                    <td className="p-3">
                      <button
                        type="button"
                        className="font-mono hover:underline"
                        onClick={() => guard(() => setOpen({ kind: 'server', id: s.id }))}
                      >
                        <bdi>{s.name}</bdi>
                      </button>
                      {s.credentials && s.credentials !== 'ok' && (
                        <span className="ml-2 text-xs text-warn">
                          {t(`servers.credentialsShort.${s.credentials}`)}
                        </span>
                      )}
                    </td>
                    <td className="p-3">
                      <bdi>{profileName(s.profile_ids)}</bdi>
                    </td>
                    {canSeeAgents && (
                      <td className="p-3 font-mono">
                        <bdi>{s.host ? `${s.host}:${s.port ?? ''}` : ''}</bdi>
                      </td>
                    )}
                    <td className="p-3 font-mono">
                      <bdi>{s.container_name ?? ''}</bdi>
                    </td>
                    <td className="p-3">
                      <bdi>{groupNames(s)}</bdi>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        )}

        <section aria-label={t('servers.serverGroups')} className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end justify-between gap-3">
            <h2 className="text-lg font-semibold">{t('servers.serverGroups')}</h2>
            {canEdit && (
              <form onSubmit={createGroup} className="flex gap-2">
                <label htmlFor={groupNameId} className="sr-only">
                  {t('servers.newGroupName')}
                </label>
                <Input
                  id={groupNameId}
                  placeholder={t('servers.newGroupName')}
                  value={groupName}
                  onChange={(e) => setGroupName(e.target.value)}
                />
                <Button type="submit" disabled={!groupName.trim()}>
                  {t('servers.createGroup')}
                </Button>
              </form>
            )}
          </div>
          {error && (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          )}
          {phone ? (
            <Cards
              label={t('servers.serverGroups')}
              empty={(groups.data ?? []).length === 0 ? t('servers.noGroups') : undefined}
            >
              {shownGroups.map((g) => (
                <Card
                  key={g.id}
                  mono
                  title={<bdi className="uppercase">{g.name}</bdi>}
                  selected={open?.id === g.id}
                  onOpen={() => guard(() => setOpen({ kind: 'group', id: g.id }))}
                  facts={[
                    {
                      label: t('nav.servers'),
                      value: (
                        <bdi>
                          {g.server_ids
                            .map((id) => all.find((s) => s.id === id)?.name ?? '')
                            .filter(Boolean)
                            .join(', ')}
                        </bdi>
                      ),
                    },
                  ]}
                />
              ))}
            </Cards>
          ) : (
            <div className="overflow-x-auto rounded-lg border border-border bg-surface">
              <table className="w-full text-sm">
                <tbody>
                  {shownGroups.map((g) => (
                    <tr
                      key={g.id}
                      className={cn(
                        'border-t border-border first:border-t-0',
                        open?.id === g.id && 'bg-surface-raised',
                      )}
                    >
                      <td className="p-3">
                        <button
                          type="button"
                          className="font-mono uppercase hover:underline"
                          onClick={() => guard(() => setOpen({ kind: 'group', id: g.id }))}
                        >
                          <bdi>{g.name}</bdi>
                        </button>
                      </td>
                      <td className="p-3">
                        <bdi>
                          {g.server_ids
                            .map((id) => all.find((s) => s.id === id)?.name ?? '')
                            .filter(Boolean)
                            .join(', ')}
                        </bdi>
                      </td>
                    </tr>
                  ))}
                  {(groups.data ?? []).length === 0 && (
                    <tr>
                      <td className="p-6 text-center text-muted">{t('servers.noGroups')}</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
      {open?.kind === 'server' && (open.id === 'new' || current) && (
        <ServerPanel
          key={open.id}
          server={current}
          canEdit={canEdit}
          canEditProfiles={canEditProfiles}
          canSeeAgents={canSeeAgents}
          onClose={() => guard(() => setOpen(null))}
          onCreated={() => setOpen(null)}
        />
      )}
      {currentGroup && (
        <GroupPanel
          key={currentGroup.id}
          group={currentGroup}
          canEdit={canEdit}
          servers={all}
          onClose={() => guard(() => setOpen(null))}
        />
      )}
    </div>
  );
}

// CertificateBlock shows the trusted certificate of a server with an
// HTTPS API, its end of validity with the warning 30 days before, and the
// certificate the last connection test read with "Trust this certificate"
// (PROT-003, D-071, D-072). Trusting only sets the draft; save sends it.
function CertificateBlock({
  trusted,
  stored,
  notAfter,
  seen,
  canTrust,
  onTrust,
  day,
}: {
  trusted: string;
  stored?: string;
  notAfter?: string;
  seen: Seen | null;
  canTrust: boolean;
  onTrust: (fp: string) => void;
  day: (iso: string) => string;
}) {
  const { t } = useTranslation();
  const notice = certNotice(notAfter, new Date());
  return (
    <div className="flex flex-col gap-1.5 rounded-md border border-border p-3 text-sm">
      <p className="font-medium">{t('servers.trustedCertificate')}</p>
      {trusted ? (
        <code className="font-mono text-xs wrap-anywhere" data-testid="trusted-fingerprint">
          {groupFingerprint(trusted)}
        </code>
      ) : (
        <p className="text-xs text-muted">–</p>
      )}
      {trusted && trusted !== stored && (
        <p className="text-xs text-warn">{t('servers.trustPending')}</p>
      )}
      {trusted && trusted === stored && (
        <p className="text-xs text-muted">
          {notAfter ? t('servers.validUntil', { date: day(notAfter) }) : t('servers.validUnknown')}
        </p>
      )}
      {notice && notAfter && (
        <p
          role="status"
          className={notice === 'expired' ? 'text-xs text-danger' : 'text-xs text-warn'}
        >
          {t(notice === 'expired' ? 'servers.certExpired' : 'servers.certSoon', {
            date: day(notAfter),
          })}
        </p>
      )}
      {seen && seen.fingerprint !== trusted && (
        <>
          <p className="mt-1 font-medium">{t('servers.shownCertificate')}</p>
          <code className="font-mono text-xs wrap-anywhere" data-testid="seen-fingerprint">
            {groupFingerprint(seen.fingerprint)}
          </code>
          {seen.notAfter && (
            <p className="text-xs text-muted">
              {t('servers.validUntil', { date: day(seen.notAfter) })}
            </p>
          )}
          {canTrust && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="self-start"
              onClick={() => onTrust(seen.fingerprint)}
            >
              {t('servers.trust')}
            </Button>
          )}
        </>
      )}
    </div>
  );
}
