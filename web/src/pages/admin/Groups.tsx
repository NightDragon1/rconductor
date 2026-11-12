// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must, patchVersioned } from '@/api/client';
import { useServerGroups, useServers } from '@/api/servers';
import {
  outcome,
  useCommandLists,
  useManagedUsers,
  userGroupsKey,
  useUserGroups,
  type UserGroup,
} from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { usePhone, useReveal } from '@/lib/phone';
import { useShortcuts } from '@/lib/shortcuts';
import { useUndo, useUnsaved } from '@/lib/undo';
import { cn } from '@/lib/utils';
import { reasonText } from '@/lib/admin';
import { Card, Cards } from './Cards';
import { PhoneNote } from './PhoneNote';

type Kind = UserGroup['kind'];

function Picker({
  label,
  options,
  onPick,
}: {
  label: string;
  options: { value: string; text: string }[];
  onPick: (v: string) => void;
}) {
  const id = useId();
  if (options.length === 0) return null;
  return (
    <>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <select
        id={id}
        className="h-9 rounded-md border border-control bg-surface px-2 text-sm"
        value=""
        onChange={(e) => e.target.value && onPick(e.target.value)}
      >
        <option value="">{label}</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.text}
          </option>
        ))}
      </select>
    </>
  );
}

function ChipList({
  label,
  items,
  onRemove,
}: {
  label: string;
  items: { key: string; text: string }[];
  onRemove?: (key: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <ul aria-label={label} className="flex flex-wrap gap-2">
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
  );
}

function GroupPanel({
  group,
  readOnly,
  onClose,
}: {
  group: UserGroup;
  readOnly: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const panel = useReveal<HTMLElement>();
  const qc = useQueryClient();
  const { saved, saving } = useUndo();
  const users = useManagedUsers();
  const lists = useCommandLists(true);
  const servers = useServers();
  const serverGroups = useServerGroups(true);
  const [draft, setDraft] = useState<UserGroup>(group);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const [removing, setRemoving] = useState(false);
  const [conflict, setConflict] = useState(false);
  // The version of the last saved state goes along as If-Match (revision
  // 2.4 candidate 24).
  const [base, setBase] = useState(group);
  const body = (g: UserGroup) => JSON.stringify([g.command_list_id ?? '', g.members, g.grants]);
  const dirty = body(draft) !== body(base);
  useUnsaved(dirty);
  const groups = useUserGroups(true);
  const listId = useId();

  const userName = (id: string) => users.data?.find((u) => u.id === id)?.pseudonym ?? id;
  // An ID the caller's lists do not know never shows as a name.
  const serverName = (id?: string) =>
    servers.data?.find((s) => s.id === id)?.name ?? t('admin.unknownServer');
  const sgName = (id?: string) =>
    serverGroups.data?.find((g) => g.id === id)?.name ?? t('admin.unknownServerGroup');
  // A moderator group takes only moderators (Design 2.2).
  const candidates = (users.data ?? []).filter(
    (u) => !draft.members.includes(u.id) && (draft.kind !== 'moderator' || u.role === 'moderator'),
  );

  // Edits change the draft; Save sends it at once (D-048).
  const write = async (from: UserGroup, to: UserGroup) => {
    let done = null as UserGroup | null;
    const r = await saving(
      outcome(group.id, async () => {
        done = must(
          await patchVersioned('/user-groups/{id}', from.version, {
            path: { id: group.id },
            body: {
              name: to.name,
              command_list_id: to.command_list_id ?? '',
              members: to.members,
              grants: to.grants,
            },
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
    void qc.invalidateQueries({ queryKey: userGroupsKey });
    return done;
  };
  const save = async () => {
    const before = base;
    const after = await write(before, draft);
    if (after)
      saved(t('groups.undo.changed', { name: after.name }), () => void write(after, before));
  };
  const change = (next: UserGroup) => {
    if (conflict) return;
    setMessage(null);
    setDraft(next);
  };

  return (
    <aside
      ref={panel}
      aria-label={t('admin.panel', { name: draft.name })}
      className="flex w-full flex-col gap-4 rounded-lg border border-border bg-surface p-5 xl:w-[28rem]"
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="wrap-anywhere md:truncate text-lg font-semibold">
            <bdi>{draft.name}</bdi>
          </h2>
          <p className="text-xs text-muted">{t(`groups.kind.${draft.kind}`)}</p>
        </div>
        <Button variant="ghost" size="sm" aria-label={t('admin.closePanel')} onClick={onClose}>
          <X aria-hidden className="size-4" />
        </Button>
      </div>

      {draft.kind === 'moderator' && (
        <div className="flex flex-col gap-1">
          <label htmlFor={listId} className="text-sm text-muted">
            {t('groups.list')}
          </label>
          <select
            id={listId}
            className="h-11 rounded-md border border-control bg-surface px-3 disabled:opacity-70"
            disabled={readOnly}
            value={draft.command_list_id ?? ''}
            onChange={(e) => change({ ...draft, command_list_id: e.target.value || undefined })}
          >
            <option value="">{t('groups.noList')}</option>
            {(lists.data ?? []).map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
        </div>
      )}

      <section aria-label={t('groups.members')} className="flex flex-col gap-2">
        <h3 className="text-sm text-muted">{t('groups.members')}</h3>
        <ChipList
          label={t('groups.members')}
          items={draft.members.map((m) => ({ key: m, text: userName(m) }))}
          onRemove={
            readOnly
              ? undefined
              : (m) => change({ ...draft, members: draft.members.filter((x) => x !== m) })
          }
        />
        {!readOnly && (
          <Picker
            label={t('groups.addMember')}
            options={candidates.map((u) => ({ value: u.id, text: u.pseudonym }))}
            onPick={(m) => change({ ...draft, members: [...draft.members, m] })}
          />
        )}
      </section>

      <section aria-label={t('groups.grants')} className="flex flex-col gap-2">
        <h3 className="text-sm text-muted">{t('groups.grants')}</h3>
        <ChipList
          label={t('groups.grants')}
          items={draft.grants.map((g) => ({
            key: g.server_id ?? g.server_group_id ?? '',
            text: g.server_id ? serverName(g.server_id) : sgName(g.server_group_id),
          }))}
          onRemove={
            readOnly
              ? undefined
              : (k) =>
                  change({
                    ...draft,
                    grants: draft.grants.filter((g) => (g.server_id ?? g.server_group_id) !== k),
                  })
          }
        />
        {!readOnly && (
          <Picker
            label={t('groups.addGrant')}
            options={[
              ...(serverGroups.data ?? [])
                .filter((g) => !draft.grants.some((x) => x.server_group_id === g.id))
                .map((g) => ({ value: `g:${g.id}`, text: g.name })),
              ...(servers.data ?? [])
                .filter((s) => !draft.grants.some((x) => x.server_id === s.id))
                .map((s) => ({ value: `s:${s.id}`, text: s.name })),
            ]}
            onPick={(v) =>
              change({
                ...draft,
                grants: [
                  ...draft.grants,
                  v.startsWith('g:') ? { server_group_id: v.slice(2) } : { server_id: v.slice(2) },
                ],
              })
            }
          />
        )}
      </section>

      {message && (
        <p
          role={message.ok ? 'status' : 'alert'}
          className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
        >
          {message.text}
        </p>
      )}
      {!readOnly && !conflict && (
        <Button className="self-start" disabled={!dirty} onClick={() => void save()}>
          {t('admin.save')}
        </Button>
      )}
      {conflict && (
        <Button
          variant="outline"
          className="self-start"
          onClick={async () => {
            const fresh = (await groups.refetch()).data?.find((x) => x.id === group.id);
            if (fresh) {
              setBase(fresh);
              setDraft(fresh);
            }
            setConflict(false);
            setMessage(null);
          }}
        >
          {t('admin.reload')}
        </Button>
      )}
      {!readOnly && (
        <Button
          variant="outline"
          className="self-start border-danger text-danger"
          onClick={() => setRemoving(true)}
        >
          {t('groups.delete')}
        </Button>
      )}
      <ConfirmDialog
        open={removing}
        title={t('groups.deleteTitle', { name: draft.name })}
        text={t('groups.deleteText')}
        action={t('groups.delete')}
        onClose={() => setRemoving(false)}
        onConfirm={async () => {
          setRemoving(false);
          const r = await outcome(group.id, async () =>
            must(await api.DELETE('/user-groups/{id}', { params: { path: { id: group.id } } })),
          );
          void qc.invalidateQueries({ queryKey: userGroupsKey });
          if (r.ok) onClose();
          else setMessage({ ok: false, text: reasonText(t, r) });
        }}
      />
    </aside>
  );
}

// GroupsPage manages user groups and moderator groups with their command
// list, members and grants (ROLE-004, ROLE-005): all for the RCON admin,
// his own for a server admin; the backend decides.
export function GroupsPage() {
  const { t } = useTranslation();
  const phone = usePhone();
  const qc = useQueryClient();
  const groups = useUserGroups(true);
  const lists = useCommandLists(true);
  const [q, setQ] = useState('');
  const [open, setOpen] = useState<string | null>(null);
  const { guard } = useUndo();
  const [name, setName] = useState('');
  const [kind, setKind] = useState<Kind>('moderator');
  const [error, setError] = useState('');
  const nameId = useId();
  const kindId = useId();
  useShortcuts({ Escape: () => guard(() => setOpen(null)) });
  const all = groups.data ?? [];
  const needle = q.trim().toLowerCase();
  const shown = all.filter((g) => !needle || g.name.toLowerCase().includes(needle));
  const current = all.find((g) => g.id === open);
  const listName = (id?: string) => lists.data?.find((l) => l.id === id)?.name ?? '';

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      const g = must(await api.POST('/user-groups', { body: { name: name.trim(), kind } }));
      setName('');
      await qc.invalidateQueries({ queryKey: userGroupsKey });
      setOpen(g.id);
    } catch (err) {
      setError(reasonText(t, (err as { problem?: { reason?: string; detail?: string } }).problem));
    }
  };

  return (
    <div className="flex flex-col gap-4 xl:flex-row xl:items-start">
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div>
          <h1 className="text-2xl font-semibold">{t('nav.groups')}</h1>
          <p className="text-sm text-muted">{t('groups.summary', { count: all.length })}</p>
        </div>
        <PhoneNote />
        <div className="flex flex-wrap gap-2">
          <Input
            type="search"
            className="w-full sm:w-72"
            placeholder={t('groups.search')}
            aria-label={t('groups.searchLabel')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          {!phone && (
            <form onSubmit={create} className="flex flex-wrap gap-2">
              <label htmlFor={nameId} className="sr-only">
                {t('groups.newName')}
              </label>
              <Input
                id={nameId}
                placeholder={t('groups.newName')}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
              <label htmlFor={kindId} className="sr-only">
                {t('groups.kindLabel')}
              </label>
              <select
                id={kindId}
                className="h-11 rounded-md border border-control bg-surface px-3"
                value={kind}
                onChange={(e) => setKind(e.target.value as Kind)}
              >
                {(['moderator', 'general'] as const).map((k) => (
                  <option key={k} value={k}>
                    {t(`groups.kind.${k}`)}
                  </option>
                ))}
              </select>
              <Button type="submit" disabled={!name.trim()}>
                {t('groups.create')}
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
            label={t('nav.groups')}
            empty={
              shown.length === 0 ? t(groups.isPending ? 'app.loading' : 'groups.none') : undefined
            }
          >
            {shown.map((g) => (
              <Card
                key={g.id}
                title={<bdi>{g.name}</bdi>}
                selected={open === g.id}
                onOpen={() => guard(() => setOpen(g.id))}
                facts={[
                  { label: t('groups.kindLabel'), value: t(`groups.kind.${g.kind}`) },
                  { label: t('groups.list'), value: <bdi>{listName(g.command_list_id)}</bdi> },
                  {
                    label: t('groups.members'),
                    value: t('groups.memberCount', { count: g.members.length }),
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
                  <th className="p-3 font-medium">{t('groups.name')}</th>
                  <th className="p-3 font-medium">{t('groups.kindLabel')}</th>
                  <th className="p-3 font-medium">{t('groups.list')}</th>
                  <th className="p-3 font-medium">{t('groups.members')}</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((g) => (
                  <tr
                    key={g.id}
                    className={cn('border-t border-border', open === g.id && 'bg-surface-raised')}
                  >
                    <td className="p-3">
                      <button
                        type="button"
                        className="hover:underline"
                        onClick={() => guard(() => setOpen(g.id))}
                      >
                        <bdi>{g.name}</bdi>
                      </button>
                    </td>
                    <td className="p-3">{t(`groups.kind.${g.kind}`)}</td>
                    <td className="p-3">
                      <bdi>{listName(g.command_list_id)}</bdi>
                    </td>
                    <td className="p-3">{t('groups.memberCount', { count: g.members.length })}</td>
                  </tr>
                ))}
                {shown.length === 0 && (
                  <tr>
                    <td colSpan={4} className="p-6 text-center text-muted">
                      {t(groups.isPending ? 'app.loading' : 'groups.none')}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {current && (
        <GroupPanel
          key={current.id}
          group={current}
          readOnly={phone}
          onClose={() => guard(() => setOpen(null))}
        />
      )}
    </div>
  );
}
