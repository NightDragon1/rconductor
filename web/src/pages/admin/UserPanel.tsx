// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import type { Server, ServerGroup } from '@/api/servers';
import {
  assignmentsKey,
  managedUsersKey,
  outcome,
  rolesBelow,
  useAssignments,
  type AssignmentsUpdate,
  type Role,
  type User,
  type UserGroup,
} from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { CopyButton } from '@/components/CopyButton';
import { Button } from '@/components/ui/button';
import { dateTime } from '@/lib/format';
import { useReveal } from '@/lib/phone';
import { useUndo } from '@/lib/undo';
import { reasonText } from '@/lib/admin';

function Chips({
  label,
  items,
  onRemove,
  add,
}: {
  label: string;
  items: { key: string; text: string }[];
  onRemove?: (key: string) => void;
  add?: { options: { value: string; text: string }[]; onAdd: (v: string) => void; label: string };
}) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <section aria-label={label} className="flex flex-col gap-2">
      <h3 className="text-sm text-muted">{label}</h3>
      <ul className="flex flex-wrap gap-2">
        {items.length === 0 && <li className="text-sm text-muted">{t('admin.none')}</li>}
        {items.map((i) => (
          <li
            key={i.key}
            className="flex items-center gap-1 rounded-md border border-border px-2 py-1 font-mono text-sm"
          >
            <bdi>{i.text}</bdi>
            {onRemove && (
              <button
                type="button"
                className="ml-1 text-muted hover:text-danger"
                aria-label={t('admin.removeItem', { name: i.text })}
                onClick={() => onRemove(i.key)}
              >
                <X aria-hidden className="size-4" />
              </button>
            )}
          </li>
        ))}
      </ul>
      {add && add.options.length > 0 && (
        <>
          <label htmlFor={id} className="sr-only">
            {add.label}
          </label>
          <select
            id={id}
            className="h-9 rounded-md border border-border bg-surface px-2 text-sm"
            value=""
            onChange={(e) => e.target.value && add.onAdd(e.target.value)}
          >
            <option value="">{add.label}</option>
            {add.options.map((o) => (
              <option key={o.value} value={o.value}>
                {o.text}
              </option>
            ))}
          </select>
        </>
      )}
    </section>
  );
}

// UserPanel shows a managed user beside the list (Design 6.1): role,
// groups, scope, supporter release and grants, and the actions. Only what
// the caller's role offers is shown; the backend decides (CLAUDE.md). On
// a phone it reads; locking and ending sessions stay, each behind a
// dialog (revision 2.4 candidate 26).
export function UserPanel({
  user,
  callerRole,
  callerId,
  callerName,
  users,
  groups,
  servers,
  serverGroups,
  readOnly = false,
  onTemplate,
  onClose,
}: {
  user: User;
  callerRole?: string;
  callerId?: string;
  callerName?: string;
  users: User[];
  groups: UserGroup[];
  servers: Server[];
  serverGroups: ServerGroup[];
  readOnly?: boolean;
  onTemplate: (u: User) => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const { schedule } = useUndo();
  const a = useAssignments(user.id);
  const [confirm, setConfirm] = useState<'role' | 'reset' | 'remove' | 'lock' | 'sessions' | null>(
    null,
  );
  const panel = useReveal<HTMLElement>();
  const [newRole, setNewRole] = useState<Role | ''>('');
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [temp, setTemp] = useState('');
  const tempRef = useRef<HTMLParagraphElement>(null);
  const roleId = useId();
  const admin = callerRole === 'rcon_admin' || callerRole === 'server_admin';

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: managedUsersKey });
    void qc.invalidateQueries({ queryKey: assignmentsKey(user.id) });
  };
  const report = async (run: () => Promise<unknown>, done: string) => {
    const r = await outcome(user.id, run);
    setMessage(r.ok ? { ok: true, text: done } : { ok: false, text: reasonText(t, r.reason) });
    refresh();
  };
  // Changes that can be undone wait UNDO_MS (K3); keepalive lets them
  // outlive the page when it goes away.
  type Send = (keepalive?: boolean) => Promise<unknown>;
  const later = (label: string, body: Send, done: string) =>
    schedule(label, (keepalive) => report(() => body(keepalive), done));
  const put =
    (body: AssignmentsUpdate): Send =>
    async (keepalive = false) =>
      must(
        await api.PUT('/users/{id}/assignments', {
          params: { path: { id: user.id } },
          body,
          keepalive,
        }),
      );
  const patch =
    (body: { disabled?: boolean; supporters_allowed?: boolean; role?: Role }): Send =>
    async (keepalive = false) =>
      must(await api.PATCH('/users/{id}', { params: { path: { id: user.id } }, body, keepalive }));

  const endSessions = () =>
    report(
      async () =>
        must(await api.DELETE('/users/{id}/sessions', { params: { path: { id: user.id } } })),
      t('admin.sessionsEnded'),
    );

  const cur = a.data;
  const groupName = (id: string) => groups.find((g) => g.id === id)?.name ?? id;
  const serverName = (id?: string) => servers.find((s) => s.id === id)?.name ?? id ?? '';
  const sgName = (id?: string) => serverGroups.find((g) => g.id === id)?.name ?? id ?? '';
  // The caller is not in his own list of managed users.
  const userName = (id: string) =>
    id === callerId ? (callerName ?? '') : (users.find((u) => u.id === id)?.pseudonym ?? '');
  const supporters = users.filter((u) => u.role === 'supporter');

  return (
    <aside
      ref={panel}
      aria-label={t('admin.panel', { name: user.pseudonym })}
      className="flex w-full flex-col gap-4 rounded-lg border border-border bg-surface p-5 xl:w-96"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="truncate font-mono text-lg font-semibold">
            <bdi>{user.pseudonym}</bdi>
          </h2>
          <p className="text-xs text-muted">
            {t('admin.created', { time: dateTime(i18n.language, user.created_at) })}
            {user.last_login_at &&
              ` · ${t('admin.lastLogin', { time: dateTime(i18n.language, user.last_login_at) })}`}
          </p>
        </div>
        <Button variant="ghost" size="sm" aria-label={t('admin.closePanel')} onClick={onClose}>
          <X aria-hidden className="size-4" />
        </Button>
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor={roleId} className="text-sm text-muted">
          {t('admin.role')}
        </label>
        <select
          id={roleId}
          className="h-11 rounded-md border border-border bg-surface px-3"
          value={user.role}
          disabled={readOnly || rolesBelow(callerRole).length < 2 || user.is_emergency}
          onChange={(e) => {
            setNewRole(e.target.value as Role);
            setConfirm('role');
          }}
        >
          {[user.role, ...rolesBelow(callerRole).filter((r) => r !== user.role)].map((r) => (
            <option key={r} value={r}>
              {t(`role.${r}`)}
            </option>
          ))}
        </select>
      </div>

      {(user.real_name || Object.keys(user.contacts).length > 0) && (
        <dl className="text-sm">
          {user.real_name && (
            <div className="flex justify-between gap-3">
              <dt className="text-muted">{t('admin.realName')}</dt>
              <dd>
                <bdi>{user.real_name}</bdi>
              </dd>
            </div>
          )}
          {Object.entries(user.contacts).map(([k, v]) => (
            <div key={k} className="flex justify-between gap-3">
              <dt className="text-muted">
                <bdi>{k}</bdi>
              </dt>
              <dd>
                <bdi>{v}</bdi>
              </dd>
            </div>
          ))}
        </dl>
      )}

      {cur && admin && (user.role === 'moderator' || cur.groups.length > 0) && (
        <Chips
          label={t('admin.groups')}
          items={cur.groups.map((g) => ({ key: g, text: groupName(g) }))}
          onRemove={
            readOnly
              ? undefined
              : (g) =>
                  later(
                    t('admin.undo.groupRemoved', { name: user.pseudonym }),
                    put({ groups: cur.groups.filter((x) => x !== g) }),
                    t('admin.saved'),
                  )
          }
          add={
            readOnly
              ? undefined
              : {
                  label: t('admin.addGroup'),
                  options: groups
                    .filter((g) => !cur.groups.includes(g.id))
                    .map((g) => ({ value: g.id, text: g.name })),
                  onAdd: (g) =>
                    later(
                      t('admin.undo.groupAdded', { name: user.pseudonym }),
                      put({ groups: [...cur.groups, g] }),
                      t('admin.saved'),
                    ),
                }
          }
        />
      )}

      {cur && user.role === 'server_admin' && (
        <Chips
          label={t('admin.scope')}
          items={cur.admin_scopes.map((s) => ({
            key: s.server_id ?? s.server_group_id ?? '',
            text: s.server_id ? serverName(s.server_id) : sgName(s.server_group_id),
          }))}
          onRemove={
            callerRole === 'rcon_admin' && !readOnly
              ? (k) =>
                  later(
                    t('admin.undo.scopeChanged', { name: user.pseudonym }),
                    put({
                      admin_scopes: cur.admin_scopes.filter(
                        (s) => (s.server_id ?? s.server_group_id) !== k,
                      ),
                    }),
                    t('admin.saved'),
                  )
              : undefined
          }
          add={
            callerRole === 'rcon_admin' && !readOnly
              ? {
                  label: t('admin.addScope'),
                  options: [
                    ...serverGroups.map((g) => ({ value: `g:${g.id}`, text: g.name })),
                    ...servers.map((s) => ({ value: `s:${s.id}`, text: s.name })),
                  ],
                  onAdd: (v) => {
                    const target = v.startsWith('g:')
                      ? { server_group_id: v.slice(2) }
                      : { server_id: v.slice(2) };
                    later(
                      t('admin.undo.scopeChanged', { name: user.pseudonym }),
                      put({ admin_scopes: [...cur.admin_scopes, target] }),
                      t('admin.saved'),
                    );
                  },
                }
              : undefined
          }
        />
      )}

      {user.role === 'moderator' && admin && (
        <label className="flex items-center justify-between gap-3 rounded-md border border-border p-3 text-sm">
          <span>
            <span className="block font-medium">{t('admin.supportersAllowed')}</span>
            <span className="text-xs text-muted">{t('admin.supportersAllowedHint')}</span>
          </span>
          <input
            type="checkbox"
            className="size-5"
            disabled={readOnly}
            checked={user.supporters_allowed}
            onChange={(e) => {
              const v = e.target.checked;
              later(
                t('admin.undo.release', { name: user.pseudonym }),
                patch({ supporters_allowed: v }),
                t('admin.saved'),
              );
            }}
          />
        </label>
      )}

      {user.role === 'moderator' && (
        <Chips
          label={t('admin.supportersOf')}
          items={supporters
            .filter((s) => s.id !== user.id)
            .filter((s) =>
              qc
                .getQueryData<{ supporter_grants: { moderator_id: string }[] }>(
                  assignmentsKey(s.id),
                )
                ?.supporter_grants.some((g) => g.moderator_id === user.id),
            )
            .map((s) => ({ key: s.id, text: s.pseudonym }))}
        />
      )}

      {cur && user.role === 'supporter' && (
        <Chips
          label={t('admin.supporterGrants')}
          items={cur.supporter_grants.map((g) => ({
            key: `${g.moderator_id}:${g.server_id}`,
            text: userName(g.moderator_id)
              ? `${serverName(g.server_id)} (${userName(g.moderator_id)})`
              : serverName(g.server_id),
          }))}
          onRemove={
            readOnly
              ? undefined
              : (k) =>
                  later(
                    t('admin.undo.grantChanged', { name: user.pseudonym }),
                    put({
                      supporter_grants: cur.supporter_grants.filter(
                        (g) => `${g.moderator_id}:${g.server_id}` !== k,
                      ),
                    }),
                    t('admin.saved'),
                  )
          }
          add={
            callerRole === 'moderator' && callerId && !readOnly
              ? {
                  label: t('admin.addServer'),
                  options: servers
                    .filter(
                      (s) =>
                        !cur.supporter_grants.some(
                          (g) => g.moderator_id === callerId && g.server_id === s.id,
                        ),
                    )
                    .map((s) => ({ value: s.id, text: s.name })),
                  onAdd: (sid) =>
                    later(
                      t('admin.undo.grantChanged', { name: user.pseudonym }),
                      put({
                        supporter_grants: [
                          ...cur.supporter_grants,
                          { moderator_id: callerId, server_id: sid },
                        ],
                      }),
                      t('admin.saved'),
                    ),
                }
              : undefined
          }
        />
      )}

      {temp && (
        <div className="flex flex-col gap-1 rounded-md border border-warn p-3">
          <p className="text-sm">{t('admin.tempPassword')}</p>
          <p ref={tempRef} className="font-mono">
            {temp}
          </p>
          <CopyButton text={temp} target={tempRef} label={t('admin.copyPassword')} />
        </div>
      )}
      {message && (
        <p role="status" className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}>
          {message.text}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        {!readOnly && (
          <Button variant="outline" size="sm" onClick={() => onTemplate(user)}>
            {t('admin.useTemplate')}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() => (readOnly ? setConfirm('sessions') : void endSessions())}
        >
          {t('admin.endSessions')}
        </Button>
        {!readOnly && !user.is_emergency && (
          <Button variant="outline" size="sm" onClick={() => setConfirm('reset')}>
            {t('admin.resetPassword')}
          </Button>
        )}
        {readOnly ? (
          !user.disabled && (
            <Button
              variant="outline"
              size="sm"
              className="border-danger text-danger"
              onClick={() => setConfirm('lock')}
            >
              {t('admin.lock')}
            </Button>
          )
        ) : (
          <Button
            variant="outline"
            size="sm"
            className={user.disabled ? '' : 'border-danger text-danger'}
            onClick={() =>
              later(
                t(user.disabled ? 'admin.undo.unlocked' : 'admin.undo.locked', {
                  name: user.pseudonym,
                }),
                patch({ disabled: !user.disabled }),
                t('admin.saved'),
              )
            }
          >
            {t(user.disabled ? 'admin.unlock' : 'admin.lock')}
          </Button>
        )}
        {!readOnly && (
          <Button
            variant="outline"
            size="sm"
            className="border-danger text-danger"
            onClick={() => setConfirm('remove')}
          >
            {t('admin.remove')}
          </Button>
        )}
      </div>

      <ConfirmDialog
        open={confirm === 'lock'}
        title={t('admin.lockTitle', { name: user.pseudonym })}
        text={t('admin.lockText')}
        action={t('admin.lock')}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          void report(patch({ disabled: true }), t('admin.saved'));
        }}
      />
      <ConfirmDialog
        open={confirm === 'sessions'}
        title={t('admin.sessionsTitle', { name: user.pseudonym })}
        text={t('admin.sessionsText')}
        action={t('admin.endSessions')}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          void endSessions();
        }}
      />

      <ConfirmDialog
        open={confirm === 'role'}
        title={t('admin.roleTitle', {
          name: user.pseudonym,
          role: newRole ? t(`role.${newRole}`) : '',
        })}
        text={t('admin.roleWarning')}
        action={t('admin.changeRole')}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          if (newRole) void report(patch({ role: newRole }), t('admin.saved'));
        }}
      />
      <ConfirmDialog
        open={confirm === 'reset'}
        title={t('admin.resetTitle', { name: user.pseudonym })}
        text={t('admin.resetText')}
        action={t('admin.resetPassword')}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          void report(async () => {
            const r = must(
              await api.POST('/users/{id}/password-reset', { params: { path: { id: user.id } } }),
            );
            setTemp(r.password);
          }, t('admin.resetDone'));
        }}
      />
      <ConfirmDialog
        open={confirm === 'remove'}
        title={t('admin.removeTitle', { count: 1 })}
        text={t('admin.removeText')}
        action={t('admin.remove')}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          setConfirm(null);
          void report(
            async () =>
              must(await api.DELETE('/users/{id}', { params: { path: { id: user.id } } })),
            t('admin.removed'),
          ).then(onClose);
        }}
      />
    </aside>
  );
}
