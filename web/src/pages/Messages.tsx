// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import { useEffect, useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import {
  messagesKey,
  type Message,
  type MessageAudience,
  type MessageComment,
} from '@/api/messages';
import { useSession } from '@/api/session';
import { useManagedUsers } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { dateTime } from '@/lib/format';
import { cn } from '@/lib/utils';

const pageSize = 50;

type Note = { ok: boolean; text: string } | null;

// Audiences each role may choose (D-085); the backend decides again.
const audiences: Record<string, MessageAudience[]> = {
  rcon_admin: ['all', 'server_admins', 'moderators', 'users'],
  server_admin: ['scope', 'scope_moderators'],
};

// failText names a refusal with the backend's own text (400, 403, 429,
// 501), so the reason shows instead of a success.
function failText(t: TFunction, e: unknown): string {
  const p = e instanceof ApiError ? e.problem : undefined;
  const why = p?.detail?.trim() || p?.title?.trim();
  return why ? t('messages.failedWith', { reason: why }) : t('messages.failed');
}

const area =
  'w-full rounded-md border border-control bg-surface px-3 py-2 text-base text-text placeholder:text-muted';

// MessagesPage lists the messages to me and from me, newest first; the
// RCON admin sees all (MSG-001 to MSG-003, D-085). Texts are foreign data
// and stand as text (SEC-006). The backend checks every request.
export function MessagesPage() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const list = useInfiniteQuery({
    queryKey: [...messagesKey, 'list'],
    queryFn: async ({ pageParam }) =>
      must(
        await api.GET('/messages', {
          params: { query: { limit: pageSize, before: pageParam } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.items.length < pageSize ? undefined : last.items[last.items.length - 1]?.id,
  });
  const [open, setOpen] = useState<string | null>(null);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const role = session?.user.role ?? '';

  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.messages')}</h1>
        <p className="text-sm text-muted">{t('messages.explain')}</p>
      </div>
      {audiences[role] && <SendForm choices={audiences[role]} />}
      <ol aria-label={t('messages.list')} className="rounded-lg border border-border bg-surface">
        {items.map((m) => (
          <MessageItem
            key={m.id}
            message={m}
            open={open === m.id}
            onToggle={() => setOpen(open === m.id ? null : m.id)}
          />
        ))}
        {list.isSuccess && items.length === 0 && (
          <li className="p-6 text-center text-muted">{t('messages.none')}</li>
        )}
        {list.isPending && <li className="p-6 text-center text-muted">{t('app.loading')}</li>}
        {list.isError && (
          <li role="alert" className="p-6 text-center text-danger">
            {failText(t, list.error)}
          </li>
        )}
      </ol>
      {list.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={list.isFetchingNextPage}
          onClick={() => void list.fetchNextPage()}
        >
          {t('messages.more')}
        </Button>
      )}
    </div>
  );
}

// SendForm is for the RCON admin and the server admin (MSG-001, MSG-002);
// moderators and supporters do not send.
function SendForm({ choices }: { choices: MessageAudience[] }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [audience, setAudience] = useState<MessageAudience>(choices[0] ?? 'all');
  const [text, setText] = useState('');
  const [picked, setPicked] = useState<string[]>([]);
  const [note, setNote] = useState<Note>(null);
  const [busy, setBusy] = useState(false);
  const ids = { audience: useId(), text: useId(), users: useId() };

  const send = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setNote(null);
    try {
      const m = must(
        await api.POST('/messages', {
          body: { text, audience, user_ids: audience === 'users' ? picked : undefined },
        }),
      );
      setText('');
      setPicked([]);
      setNote({
        ok: true,
        text:
          m.recipient_count === undefined
            ? t('messages.sent')
            : t('messages.sentTo', { count: m.recipient_count }),
      });
      await qc.invalidateQueries({ queryKey: messagesKey });
    } catch (err) {
      setNote({ ok: false, text: failText(t, err) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      aria-label={t('messages.new')}
      onSubmit={(e) => void send(e)}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
    >
      <h2 className="font-semibold">{t('messages.new')}</h2>
      <label htmlFor={ids.audience} className="text-sm text-muted">
        {t('messages.to')}
      </label>
      <select
        id={ids.audience}
        className="h-11 rounded-md border border-control bg-surface px-2 text-base"
        value={audience}
        onChange={(e) => setAudience(e.target.value as MessageAudience)}
      >
        {choices.map((a) => (
          <option key={a} value={a}>
            {t(`messages.audience.${a}`)}
          </option>
        ))}
      </select>
      {audience === 'users' && <UserPicker id={ids.users} picked={picked} onChange={setPicked} />}
      <label htmlFor={ids.text} className="text-sm text-muted">
        {t('messages.text')}
      </label>
      <textarea
        id={ids.text}
        rows={4}
        className={area}
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      {note && (
        <p
          role={note.ok ? 'status' : 'alert'}
          className={cn('text-sm wrap-anywhere', note.ok ? 'text-ok' : 'text-danger')}
        >
          {note.text}
        </p>
      )}
      <Button type="submit" className="self-start" disabled={busy || text.trim() === ''}>
        {t('messages.send')}
      </Button>
    </form>
  );
}

// UserPicker lists the users the RCON admin manages (GET /users).
function UserPicker({
  id,
  picked,
  onChange,
}: {
  id: string;
  picked: string[];
  onChange: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const users = useManagedUsers();
  return (
    <fieldset aria-labelledby={id} className="flex flex-col gap-1">
      <legend id={id} className="text-sm text-muted">
        {t('messages.users')}
      </legend>
      <ul className="max-h-48 overflow-y-auto rounded-md border border-control">
        {(users.data ?? []).map((u) => (
          <li key={u.id}>
            <label className="flex min-h-11 items-center gap-3 px-3">
              <input
                type="checkbox"
                className="size-5"
                checked={picked.includes(u.id)}
                onChange={(e) =>
                  onChange(e.target.checked ? [...picked, u.id] : picked.filter((x) => x !== u.id))
                }
              />
              <bdi className="wrap-anywhere">{u.pseudonym}</bdi>
              <span className="text-xs text-muted">{t(`role.${u.role}`)}</span>
            </label>
          </li>
        ))}
        {users.isSuccess && users.data.length === 0 && (
          <li className="p-3 text-sm text-muted">{t('messages.noUsers')}</li>
        )}
      </ul>
    </fieldset>
  );
}

function MessageItem({
  message: m,
  open,
  onToggle,
}: {
  message: Message;
  open: boolean;
  onToggle: () => void;
}) {
  const { t, i18n } = useTranslation();
  const { data: session } = useSession();
  const mine = !!m.sender.id && m.sender.id === session?.user.id;
  return (
    <li className="border-t border-border first:border-t-0" data-testid="message">
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full flex-col gap-1 px-4 py-3 text-left text-sm hover:bg-surface-raised"
      >
        <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          {!m.read && (
            <span className="rounded bg-accent px-1.5 text-xs text-accent-text">
              {t('messages.unreadMark')}
            </span>
          )}
          <bdi className="font-semibold wrap-anywhere">{m.sender.pseudonym}</bdi>
          {mine && <span className="text-xs text-muted">{t('messages.mine')}</span>}
          <time dateTime={m.created_at} className="font-mono text-muted">
            {dateTime(i18n.language, m.created_at)}
          </time>
          <span className="rounded border border-border px-1.5 text-xs">
            {t(`messages.audience.${m.audience}`)}
          </span>
          <span className="text-xs text-muted">
            {t('messages.comments', { count: m.comment_count })}
          </span>
        </span>
        {!open && (
          <span className="line-clamp-2 whitespace-pre-wrap wrap-anywhere">
            <ForeignText text={m.text} />
          </span>
        )}
      </button>
      {open && <MessageDetail message={m} mine={mine} />}
    </li>
  );
}

// MessageDetail shows the whole text, marks it read for a recipient and
// offers deleting to the sender and the RCON admin (D-085).
function MessageDetail({ message: m, mine }: { message: Message; mine: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: session } = useSession();
  const [confirm, setConfirm] = useState(false);
  const [note, setNote] = useState<Note>(null);
  const admin = session?.user.role === 'rcon_admin';

  // A message I did not send is to me; reading it marks it read. A refusal
  // leaves it unread, as the list shows.
  useEffect(() => {
    if (mine || m.read) return;
    void api.POST('/messages/{id}/read', { params: { path: { id: m.id } } }).then((r) => {
      if (r.response.ok) void qc.invalidateQueries({ queryKey: messagesKey });
    });
  }, [m.id, m.read, mine, qc]);

  const remove = async () => {
    setConfirm(false);
    try {
      must(await api.DELETE('/messages/{id}', { params: { path: { id: m.id } } }));
      await qc.invalidateQueries({ queryKey: messagesKey });
    } catch (err) {
      setNote({ ok: false, text: failText(t, err) });
    }
  };

  return (
    <div className="flex flex-col gap-3 px-4 pb-4 text-sm">
      <p className="whitespace-pre-wrap wrap-anywhere">
        <ForeignText text={m.text} />
      </p>
      {m.recipient_count !== undefined && (
        <p className="text-muted">{t('messages.recipients', { count: m.recipient_count })}</p>
      )}
      {note && (
        <p role="alert" className="text-danger wrap-anywhere">
          {note.text}
        </p>
      )}
      {(mine || admin) && (
        <Button variant="outline" className="self-start" onClick={() => setConfirm(true)}>
          {t('messages.delete')}
        </Button>
      )}
      <ConfirmDialog
        open={confirm}
        title={t('messages.deleteTitle')}
        text={t('messages.deleteText')}
        action={t('messages.delete')}
        onConfirm={() => void remove()}
        onClose={() => setConfirm(false)}
      />
      <Comments message={m} mine={mine} />
    </div>
  );
}

// Comments of a message (MSG-003): recipients and the sender comment;
// the author, the sender and the RCON admin delete a comment.
function Comments({ message: m, mine }: { message: Message; mine: boolean }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const { data: session } = useSession();
  const key = [...messagesKey, m.id, 'comments'];
  const list = useInfiniteQuery({
    queryKey: key,
    queryFn: async ({ pageParam }) =>
      must(
        await api.GET('/messages/{id}/comments', {
          params: { path: { id: m.id }, query: { limit: pageSize, after: pageParam } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.items.length < pageSize ? undefined : last.items[last.items.length - 1]?.id,
  });
  const [text, setText] = useState('');
  const [note, setNote] = useState<Note>(null);
  const [busy, setBusy] = useState(false);
  const textId = useId();
  const me = session?.user.id;
  const admin = session?.user.role === 'rcon_admin';
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const mayDelete = (c: MessageComment) => admin || mine || (!!c.author.id && c.author.id === me);

  const changed = () => qc.invalidateQueries({ queryKey: messagesKey });

  const send = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setNote(null);
    try {
      must(
        await api.POST('/messages/{id}/comments', {
          params: { path: { id: m.id } },
          body: { text },
        }),
      );
      setText('');
      await changed();
    } catch (err) {
      setNote({ ok: false, text: failText(t, err) });
    } finally {
      setBusy(false);
    }
  };

  const remove = async (c: MessageComment) => {
    setNote(null);
    try {
      must(
        await api.DELETE('/messages/{id}/comments/{comment_id}', {
          params: { path: { id: m.id, comment_id: c.id } },
        }),
      );
      await changed();
    } catch (err) {
      setNote({ ok: false, text: failText(t, err) });
    }
  };

  return (
    <section aria-label={t('messages.commentsLabel')} className="flex flex-col gap-2">
      <h3 className="font-semibold">{t('messages.commentsLabel')}</h3>
      {list.isError && (
        <p role="alert" className="text-danger wrap-anywhere">
          {failText(t, list.error)}
        </p>
      )}
      {items.length > 0 && (
        <ol className="flex flex-col gap-2">
          {items.map((c) => (
            <li
              key={c.id}
              className="flex flex-col gap-1 rounded-md bg-surface-raised p-3"
              data-testid="comment"
            >
              <span className="flex flex-wrap items-baseline gap-x-3">
                <bdi className="font-semibold wrap-anywhere">{c.author.pseudonym}</bdi>
                <time dateTime={c.created_at} className="font-mono text-muted">
                  {dateTime(i18n.language, c.created_at)}
                </time>
                {mayDelete(c) && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="ml-auto"
                    onClick={() => void remove(c)}
                  >
                    {t('messages.deleteComment')}
                  </Button>
                )}
              </span>
              <span className="whitespace-pre-wrap wrap-anywhere">
                <ForeignText text={c.text} />
              </span>
            </li>
          ))}
        </ol>
      )}
      {list.isSuccess && items.length === 0 && (
        <p className="text-muted">{t('messages.noComments')}</p>
      )}
      {list.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={list.isFetchingNextPage}
          onClick={() => void list.fetchNextPage()}
        >
          {t('messages.moreComments')}
        </Button>
      )}
      <form onSubmit={(e) => void send(e)} className="flex flex-col gap-2">
        <label htmlFor={textId} className="text-muted">
          {t('messages.comment')}
        </label>
        <textarea
          id={textId}
          rows={2}
          className={area}
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        {note && (
          <p role="alert" className="text-danger wrap-anywhere">
            {note.text}
          </p>
        )}
        <Button type="submit" className="self-start" disabled={busy || text.trim() === ''}>
          {t('messages.sendComment')}
        </Button>
      </form>
    </section>
  );
}
