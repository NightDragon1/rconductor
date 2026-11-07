// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must } from '@/api/client';
import { useServerGroups, useServers } from '@/api/servers';
import { useSession } from '@/api/session';
import {
  assignmentsKey,
  fetchAssignments,
  invitesKey,
  managedUsersKey,
  outcome,
  useAllAssignments,
  useInvites,
  useManagedUsers,
  useUserGroups,
  type Assignments,
  type AssignmentsUpdate,
  type Outcome,
  type Role,
  type User,
} from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ago, span, useNow } from '@/lib/format';
import { usePhone } from '@/lib/phone';
import { useShortcuts } from '@/lib/shortcuts';
import { useUndo } from '@/lib/undo';
import { cn } from '@/lib/utils';
import { BulkResults } from './BulkResults';
import { Card, Cards } from './Cards';
import { InviteDialog } from './InviteDialog';
import { PhoneNote } from './PhoneNote';
import { UserPanel } from './UserPanel';

type Status = 'all' | 'active' | 'locked' | 'invited';

const initials = (p: string) =>
  p
    .replace(/[^\p{L}\p{N}]/gu, '')
    .slice(0, 2)
    .toUpperCase();

// UsersPage is the user administration (UI-007, UI-008, AUTH-009): the
// managed users and open invitations with search, filters, multiple
// selection with mass actions and a side panel. The backend limits all to
// the caller's area (Design 3.6); the page only hides. A phone reads;
// locking, unlocking and ending sessions stay in the panel (UI-012, revision 2.4
// candidate 26).
export function UsersPage() {
  const { t } = useTranslation();
  const phone = usePhone();
  const qc = useQueryClient();
  const now = useNow(30_000);
  const { saved, undoLast, guard } = useUndo();
  const { data: session } = useSession();
  const role = session?.user.role;
  const admin = role === 'rcon_admin' || role === 'server_admin';
  const users = useManagedUsers();
  const invites = useInvites();
  const groups = useUserGroups(admin);
  const servers = useServers();
  const serverGroups = useServerGroups(admin);
  const list = users.data ?? [];
  const assignments = useAllAssignments(list.map((u) => u.id));
  const byId = new Map<string, Assignments>();
  list.forEach((u, i) => {
    const d = assignments[i]?.data;
    if (d) byId.set(u.id, d);
  });

  const [q, setQ] = useState('');
  const [roleFilter, setRoleFilter] = useState<Role | 'all'>('all');
  const [status, setStatus] = useState<Status>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState<string | null>(null);
  const [locking, setLocking] = useState(false);
  const [invite, setInvite] = useState<{ template?: User; key: number } | null>(null);
  const [results, setResults] = useState<{ title: string; items: Outcome[] } | null>(null);
  const [removing, setRemoving] = useState(false);
  const search = useRef<HTMLInputElement>(null);
  const roleId = useId();
  const statusId = useId();
  const groupId = useId();
  const scopeId = useId();

  useShortcuts({
    '/': () => search.current?.focus(),
    i: () => !phone && setInvite({ key: Date.now() }),
    Escape: () => guard(() => setOpen(null)),
    z: undoLast,
  });

  const groupNames = new Map((groups.data ?? []).map((g) => [g.id, g.name]));
  const serverNames = new Map((servers.data ?? []).map((s) => [s.id, s.name]));
  const sgNames = new Map((serverGroups.data ?? []).map((g) => [g.id, g.name]));
  const scopeText = (u: User): string => {
    const a = byId.get(u.id);
    if (u.role === 'rcon_admin') return t('admin.allServers');
    if (!a) return '';
    return [
      ...a.admin_scopes.map(
        (s) =>
          (s.server_id ? serverNames.get(s.server_id) : sgNames.get(s.server_group_id ?? '')) ?? '',
      ),
      ...a.groups.map((g) => groupNames.get(g) ?? ''),
      ...a.supporter_grants.map((g) => serverNames.get(g.server_id) ?? ''),
    ]
      .filter(Boolean)
      .join(', ');
  };

  const needle = q.trim().toLowerCase();
  const shown = list.filter(
    (u) =>
      (roleFilter === 'all' || u.role === roleFilter) &&
      (status === 'all' ||
        (status === 'locked' ? u.disabled : status === 'active' ? !u.disabled : false)) &&
      (!needle ||
        u.pseudonym.toLowerCase().includes(needle) ||
        (u.real_name ?? '').toLowerCase().includes(needle) ||
        scopeText(u).toLowerCase().includes(needle)),
  );
  const openInvites = (invites.data ?? []).filter(
    (i) =>
      !i.used_at &&
      (roleFilter === 'all' || i.role === roleFilter) &&
      (status === 'all' || status === 'invited'),
  );
  const chosen = list.filter((u) => selected.has(u.id));
  const panelUser = list.find((u) => u.id === open);

  // A mass action runs per user and reports each outcome (UI-007): no
  // overall success when some fail.
  // run gives back how to restore the user, for Undo (D-048).
  type Restore = () => Promise<unknown>;
  const bulk = async (title: string, run: (u: User) => Promise<Restore | undefined>) => {
    const targets = chosen;
    const restores: Restore[] = [];
    const refresh = () => {
      void qc.invalidateQueries({ queryKey: managedUsersKey });
      targets.forEach((u) => void qc.invalidateQueries({ queryKey: assignmentsKey(u.id) }));
    };
    const items = await Promise.all(
      targets.map((u) =>
        outcome(u.id, async () => {
          const r = await run(u);
          if (r) restores.push(r);
        }),
      ),
    );
    setResults({ title, items });
    refresh();
    if (restores.length === 0) return;
    saved(t('admin.undo.bulk', { action: title, count: restores.length }), () => {
      void Promise.all(restores.map((r) => r())).finally(refresh);
    });
  };
  const setDisabled = async (u: User, disabled: boolean) =>
    must(await api.PATCH('/users/{id}', { params: { path: { id: u.id } }, body: { disabled } }));
  const lock = () =>
    bulk(t('admin.lock'), async (u) => {
      await setDisabled(u, true);
      return undefined;
    });
  const unlock = () =>
    bulk(t('admin.unlock'), async (u) => {
      const was = u.disabled;
      await setDisabled(u, false);
      return () => setDisabled(u, was);
    });
  const assign = async (u: User, body: AssignmentsUpdate) =>
    must(await api.PUT('/users/{id}/assignments', { params: { path: { id: u.id } }, body }));
  const addGroup = (gid: string) =>
    bulk(t('admin.bulk.groupTitle', { name: groupNames.get(gid) ?? '' }), async (u) => {
      const cur = await fetchAssignments(u.id);
      await assign(u, { groups: [...new Set([...cur.groups, gid])] });
      return () => assign(u, { groups: cur.groups });
    });
  const addScope = (v: string) => {
    const target = v.startsWith('g:') ? { server_group_id: v.slice(2) } : { server_id: v.slice(2) };
    const name = v.startsWith('g:') ? sgNames.get(v.slice(2)) : serverNames.get(v.slice(2));
    void bulk(t('admin.bulk.scopeTitle', { name: name ?? '' }), async (u) => {
      const cur = await fetchAssignments(u.id);
      await assign(u, { admin_scopes: [...cur.admin_scopes, target] });
      return () => assign(u, { admin_scopes: cur.admin_scopes });
    });
  };

  const toggle = (id: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const select = 'h-9 max-md:h-11 rounded-md border border-control bg-surface px-2 text-sm';
  const locked = list.filter((u) => u.disabled).length;
  const pendingInvites = (invites.data ?? []).filter(
    (i) => !i.used_at && Date.parse(i.expires_at) > now,
  ).length;

  return (
    <div className="flex flex-col gap-4 xl:flex-row xl:items-start">
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-2xl font-semibold">{t('nav.users')}</h1>
            <p className="text-sm text-muted">
              {[
                t('admin.summary', { count: list.length, locked }),
                t('admin.invitesOpen', { count: pendingInvites }),
              ].join(' · ')}
            </p>
          </div>
          {!phone && (
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                onClick={() => setInvite({ key: Date.now(), template: chosen[0] ?? list[0] })}
              >
                {t('admin.invite.fromTemplate')}
              </Button>
              <Button onClick={() => setInvite({ key: Date.now() })}>
                {t('admin.invite.title')}
              </Button>
            </div>
          )}
        </div>
        <PhoneNote users />

        <div className="flex flex-wrap gap-2">
          <Input
            ref={search}
            type="search"
            className="w-full sm:w-72"
            placeholder={t('admin.search')}
            aria-label={t('admin.searchLabel')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <label htmlFor={roleId} className="sr-only">
            {t('admin.filterRole')}
          </label>
          <select
            id={roleId}
            className={select}
            value={roleFilter}
            onChange={(e) => setRoleFilter(e.target.value as Role | 'all')}
          >
            <option value="all">{t('admin.allRoles')}</option>
            {(['server_admin', 'moderator', 'supporter'] as const).map((r) => (
              <option key={r} value={r}>
                {t(`role.${r}`)}
              </option>
            ))}
          </select>
          <label htmlFor={statusId} className="sr-only">
            {t('admin.filterStatus')}
          </label>
          <select
            id={statusId}
            className={select}
            value={status}
            onChange={(e) => setStatus(e.target.value as Status)}
          >
            {(['all', 'active', 'locked', 'invited'] as const).map((s) => (
              <option key={s} value={s}>
                {t(`admin.status.${s}`)}
              </option>
            ))}
          </select>
        </div>

        {chosen.length > 0 && !phone && (
          <div
            role="toolbar"
            aria-label={t('admin.bulk.label')}
            className="flex flex-wrap items-center gap-2 rounded-lg border border-accent bg-surface-raised p-3"
          >
            <span className="font-semibold">
              {t('admin.bulk.selected', { count: chosen.length })}
            </span>
            {admin && (groups.data ?? []).length > 0 && (
              <>
                <label htmlFor={groupId} className="sr-only">
                  {t('admin.bulk.group')}
                </label>
                <select
                  id={groupId}
                  className={select}
                  value=""
                  onChange={(e) => e.target.value && addGroup(e.target.value)}
                >
                  <option value="">{t('admin.bulk.group')}</option>
                  {(groups.data ?? []).map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              </>
            )}
            {role === 'rcon_admin' && (
              <>
                <label htmlFor={scopeId} className="sr-only">
                  {t('admin.bulk.scope')}
                </label>
                <select
                  id={scopeId}
                  className={select}
                  value=""
                  onChange={(e) => e.target.value && addScope(e.target.value)}
                >
                  <option value="">{t('admin.bulk.scope')}</option>
                  {(serverGroups.data ?? []).map((g) => (
                    <option key={g.id} value={`g:${g.id}`}>
                      {g.name}
                    </option>
                  ))}
                  {(servers.data ?? []).map((s) => (
                    <option key={s.id} value={`s:${s.id}`}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </>
            )}
            <Button variant="outline" size="sm" onClick={() => setLocking(true)}>
              {t('admin.lock')}
            </Button>
            <Button variant="outline" size="sm" onClick={() => void unlock()}>
              {t('admin.unlock')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="border-danger text-danger"
              onClick={() => setRemoving(true)}
            >
              {t('admin.remove')}
            </Button>
          </div>
        )}

        {phone ? (
          <Cards
            label={t('nav.users')}
            empty={
              shown.length === 0 && openInvites.length === 0
                ? t(users.isPending ? 'app.loading' : 'admin.noMatch')
                : undefined
            }
          >
            {shown.map((u) => (
              <Card
                key={u.id}
                mono
                title={<bdi>{u.pseudonym}</bdi>}
                selected={open === u.id}
                onOpen={() => guard(() => setOpen(u.id))}
                facts={[
                  { label: t('admin.role'), value: t(`role.${u.role}`) },
                  {
                    label: t('admin.statusLabel'),
                    value: t(u.disabled ? 'admin.status.locked' : 'admin.status.active'),
                    className: u.disabled ? 'text-danger' : 'text-ok',
                  },
                ]}
              />
            ))}
            {openInvites.map((i) => {
              const expired = Date.parse(i.expires_at) <= now;
              return (
                <Card
                  key={i.id}
                  mono
                  title={t('admin.invitation')}
                  facts={[
                    { label: t('admin.role'), value: t(`role.${i.role}`) },
                    {
                      label: t('admin.statusLabel'),
                      value: t(expired ? 'admin.expired' : 'admin.status.invited'),
                      className: expired ? 'text-muted' : 'text-warn',
                    },
                  ]}
                />
              );
            })}
          </Cards>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border bg-surface">
            <table className="w-full text-sm">
              <thead className="text-left text-muted">
                <tr>
                  <th className="w-10 p-3">
                    <input
                      type="checkbox"
                      aria-label={t('admin.selectAll')}
                      checked={shown.length > 0 && shown.every((u) => selected.has(u.id))}
                      onChange={(e) =>
                        setSelected(e.target.checked ? new Set(shown.map((u) => u.id)) : new Set())
                      }
                    />
                  </th>
                  <th className="p-3 font-medium">{t('login.pseudonym')}</th>
                  <th className="p-3 font-medium">{t('admin.role')}</th>
                  <th className="p-3 font-medium">{t('admin.groupsScope')}</th>
                  <th className="p-3 font-medium">{t('admin.lastActive')}</th>
                  <th className="p-3 font-medium">{t('admin.statusLabel')}</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((u) => (
                  <tr
                    key={u.id}
                    aria-selected={open === u.id}
                    className={cn(
                      'border-t border-border',
                      (selected.has(u.id) || open === u.id) && 'bg-surface-raised',
                    )}
                  >
                    <td className="p-3">
                      <input
                        type="checkbox"
                        aria-label={t('admin.select', { name: u.pseudonym })}
                        checked={selected.has(u.id)}
                        onChange={() => toggle(u.id)}
                      />
                    </td>
                    <td className="p-3">
                      <button
                        type="button"
                        className="flex items-center gap-2 font-mono hover:underline"
                        onClick={() => guard(() => setOpen(u.id))}
                      >
                        <span
                          className="flex size-7 items-center justify-center rounded-full bg-surface-raised text-xs"
                          aria-hidden
                        >
                          {initials(u.pseudonym)}
                        </span>
                        <bdi>{u.pseudonym}</bdi>
                      </button>
                    </td>
                    <td className="p-3">
                      <span className="rounded border border-border px-1.5 text-xs">
                        {t(`role.${u.role}`)}
                      </span>
                    </td>
                    <td className="max-w-64 wrap-anywhere md:truncate p-3">
                      <bdi>{scopeText(u)}</bdi>
                    </td>
                    <td className="p-3 text-muted">
                      {u.last_login_at ? ago(t, u.last_login_at, now) : t('admin.never')}
                    </td>
                    <td className={cn('p-3', u.disabled ? 'text-danger' : 'text-ok')}>
                      {t(u.disabled ? 'admin.status.locked' : 'admin.status.active')}
                    </td>
                  </tr>
                ))}
                {openInvites.map((i) => {
                  const expired = Date.parse(i.expires_at) <= now;
                  return (
                    <tr key={i.id} className="border-t border-border">
                      <td className="p-3" />
                      <td className="p-3 font-mono text-muted">{t('admin.invitation')}</td>
                      <td className="p-3">
                        <span className="rounded border border-border px-1.5 text-xs">
                          {t(`role.${i.role}`)}
                        </span>
                      </td>
                      <td className="p-3" />
                      <td className="p-3 text-muted">
                        {!expired &&
                          t('admin.expiresIn', { span: span(t, Date.parse(i.expires_at) - now) })}
                      </td>
                      <td className="p-3">
                        <span className={expired ? 'text-muted' : 'text-warn'}>
                          {t(expired ? 'admin.expired' : 'admin.status.invited')}
                        </span>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="ml-2"
                          onClick={async () => {
                            await outcome(i.id, async () =>
                              must(
                                await api.DELETE('/invites/{id}', {
                                  params: { path: { id: i.id } },
                                }),
                              ),
                            );
                            void qc.invalidateQueries({ queryKey: invitesKey });
                          }}
                        >
                          {t('admin.withdraw')}
                        </Button>
                      </td>
                    </tr>
                  );
                })}
                {shown.length === 0 && openInvites.length === 0 && (
                  <tr>
                    <td colSpan={6} className="p-6 text-center text-muted">
                      {t(users.isPending ? 'app.loading' : 'admin.noMatch')}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {panelUser && (
        <UserPanel
          key={panelUser.id}
          user={panelUser}
          callerRole={role}
          callerId={session?.user.id}
          callerName={session?.user.pseudonym}
          users={list}
          groups={groups.data ?? []}
          servers={servers.data ?? []}
          serverGroups={serverGroups.data ?? []}
          readOnly={phone}
          onTemplate={(u) => setInvite({ key: Date.now(), template: u })}
          onClose={() => guard(() => setOpen(null))}
        />
      )}

      {invite && (
        <InviteDialog
          key={invite.key}
          open
          callerRole={role}
          users={list}
          template={invite.template}
          onClose={() => setInvite(null)}
        />
      )}
      <BulkResults
        title={results?.title ?? ''}
        results={results?.items ?? null}
        users={list}
        onClose={() => {
          setResults(null);
          setSelected(new Set());
        }}
      />
      <ConfirmDialog
        open={removing}
        title={t('admin.removeTitle', { count: chosen.length })}
        text={t('admin.removeText')}
        action={t('admin.remove')}
        onClose={() => setRemoving(false)}
        onConfirm={() => {
          setRemoving(false);
          void bulk(t('admin.remove'), async (u) => {
            must(await api.DELETE('/users/{id}', { params: { path: { id: u.id } } }));
            return undefined;
          });
        }}
      />
      <ConfirmDialog
        open={locking}
        title={t('admin.lockManyTitle', { count: chosen.length })}
        text={t('admin.lockText')}
        action={t('admin.lock')}
        onClose={() => setLocking(false)}
        onConfirm={() => {
          setLocking(false);
          void lock();
        }}
      />
    </div>
  );
}
