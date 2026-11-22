// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import { ArrowLeft, Plus } from 'lucide-react';
import { useEffect, useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import {
  messagesKey,
  messageTitle,
  type Message,
  type MessageAudience,
  type MessageComment,
} from '@/api/messages';
import { useSession } from '@/api/session';
import { useManagedUsers } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { ForeignText } from '@/components/ForeignText';
import { Button } from '@/components/ui/button';
import { Dialog, DialogClose, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { dateTime } from '@/lib/format';
import { cn } from '@/lib/utils';
import { PageHeader } from '@/components/PageHeader';

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

// MessagesPage shows the messages to me and from me as conversations,
// newest first: the list on the left, the chosen message with its comments
// on the right; below 768 px the list and the message take turns. The RCON
// admin sees all (MSG-001 to MSG-003, D-085). Texts are foreign data and
// stand as text (SEC-006). The backend checks every request.
export function MessagesPage() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const list = useInfiniteQuery({
    queryKey: [...messagesKey, 'list'],
    queryFn: async ({ pageParam }) =>
      must(
        await api.GET('/messages', {
          params: { query: { limit: pageSize, cursor: pageParam } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    // The backend says whether more follow (MSG-001): no extra request
    // after a page of exactly pageSize, and none lost after a delete.
    getNextPageParam: (last) => last.next_cursor,
  });
  const [open, setOpen] = useState<string | null>(null);
  const [compose, setCompose] = useState(false);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const role = session?.user.role ?? '';
  const choices = audiences[role];
  // A deleted message is gone from the list, and so from the view.
  const current = items.find((m) => m.id === open);
  const empty = list.isSuccess && items.length === 0;

  return (
    <div className="flex max-w-6xl flex-col gap-4">
      <PageHeader
        title={t('nav.messages')}
        description={t('messages.explain')}
        actions={
          choices && (
            <Button onClick={() => setCompose(true)}>
              <Plus aria-hidden />
              {t('messages.new')}
            </Button>
          )
        }
      />
      {choices && (
        <Dialog open={compose} onOpenChange={setCompose}>
          <DialogContent
            aria-describedby={undefined}
            className="max-h-[calc(100dvh-2rem)] overflow-y-auto"
          >
            <DialogTitle>{t('messages.new')}</DialogTitle>
            <SendForm choices={choices} />
            <div className="flex justify-end">
              <DialogClose asChild>
                <Button variant="ghost">{t('messages.close')}</Button>
              </DialogClose>
            </div>
          </DialogContent>
        </Dialog>
      )}
      <div className="grid gap-4 md:grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)]">
        <div className={cn('flex min-w-0 flex-col gap-2', current && 'max-md:hidden')}>
          {items.length > 0 && (
            <ol
              aria-label={t('messages.list')}
              className="rounded-lg border border-border bg-surface md:max-h-[70dvh] md:overflow-y-auto"
            >
              {items.map((m) => (
                <MessageItem
                  key={m.id}
                  message={m}
                  active={current?.id === m.id}
                  onOpen={() => setOpen(m.id)}
                />
              ))}
            </ol>
          )}
          {empty && (
            <p className="rounded-lg border border-border bg-surface p-6 text-center text-muted">
              {t('messages.none')}
            </p>
          )}
          {list.isPending && <p className="p-6 text-center text-muted">{t('app.loading')}</p>}
          {list.isError && (
            <p role="alert" className="p-6 text-center text-danger">
              {failText(t, list.error)}
            </p>
          )}
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
        <div className={cn('min-w-0', !current && 'max-md:hidden')}>
          {current ? (
            <MessageView key={current.id} message={current} onBack={() => setOpen(null)} />
          ) : (
            <div className="flex min-h-48 flex-col items-center justify-center gap-1 rounded-lg border border-border bg-surface p-6 text-center">
              <p className="text-muted">{t('messages.noneSelected')}</p>
              <p className="text-sm text-muted">{t('messages.pick')}</p>
            </div>
          )}
        </div>
      </div>
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
      className="flex flex-col gap-3"
    >
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

// MessageItem is one conversation in the list: title, sender and time;
// an unread one is bold.
function MessageItem({
  message: m,
  active,
  onOpen,
}: {
  message: Message;
  active: boolean;
  onOpen: () => void;
}) {
  const { t, i18n } = useTranslation();
  return (
    <li className="border-t border-border first:border-t-0" data-testid="message">
      <button
        type="button"
        aria-current={active || undefined}
        onClick={onOpen}
        className={cn(
          'flex min-h-11 w-full flex-col gap-1 px-4 py-3 text-left text-sm hover:bg-surface-raised',
          active && 'bg-surface-raised',
          !m.read && 'font-semibold',
        )}
      >
        <span className="truncate">
          <ForeignText text={messageTitle(m.text)} />
        </span>
        <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs">
          {!m.read && (
            <span className="rounded bg-accent px-1.5 text-accent-text">
              {t('messages.unreadMark')}
            </span>
          )}
          <bdi className="wrap-anywhere">{m.sender.pseudonym}</bdi>
          <time dateTime={m.created_at} className="font-mono text-muted">
            {dateTime(i18n.language, m.created_at)}
          </time>
          {m.comment_count > 0 && (
            <span className="text-muted">{t('messages.comments', { count: m.comment_count })}</span>
          )}
        </span>
      </button>
    </li>
  );
}

// Audiences whose recipients never include the RCON admin (D-085).
const notToAdmin: string[] = ['server_admins', 'moderators', 'scope', 'scope_moderators'];

// MessageView shows the whole message on top and its comments below as a
// thread, marks it read for a recipient and offers deleting to the sender
// and the RCON admin (D-085). Below 768 px it leads back to the list.
function MessageView({ message: m, onBack }: { message: Message; onBack: () => void }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const { data: session } = useSession();
  const [confirm, setConfirm] = useState(false);
  const [note, setNote] = useState<Note>(null);
  const admin = session?.user.role === 'rcon_admin';
  const mine = !!m.sender.id && m.sender.id === session?.user.id;

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
      onBack();
      await qc.invalidateQueries({ queryKey: messagesKey });
    } catch (err) {
      setNote({ ok: false, text: failText(t, err) });
    }
  };

  return (
    <article
      aria-label={t('messages.message')}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4 text-sm"
    >
      <Button variant="ghost" className="self-start md:hidden" onClick={onBack}>
        <ArrowLeft aria-hidden />
        {t('messages.back')}
      </Button>
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <bdi className="font-semibold wrap-anywhere">{m.sender.pseudonym}</bdi>
        {mine && <span className="text-xs text-muted">{t('messages.mine')}</span>}
        <time dateTime={m.created_at} className="font-mono text-muted">
          {dateTime(i18n.language, m.created_at)}
        </time>
        <span className="rounded border border-border px-1.5 text-xs">
          {t(`messages.audience.${m.audience}`)}
        </span>
        {m.recipient_count !== undefined && (
          <span className="text-xs text-muted">
            {t('messages.recipients', { count: m.recipient_count })}
          </span>
        )}
      </header>
      <p className="whitespace-pre-wrap wrap-anywhere">
        <ForeignText text={m.text} />
      </p>
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
    </article>
  );
}

// Comments of a message (MSG-003) as a thread, the reply field at the
// bottom: a reply is a comment. Recipients and the sender comment; the
// author, the sender and the RCON admin delete a comment.
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
          params: { path: { id: m.id }, query: { limit: pageSize, cursor: pageParam } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    // The backend says whether more follow (MSG-001): no extra request
    // after a page of exactly pageSize, and none lost after a delete.
    getNextPageParam: (last) => last.next_cursor,
  });
  const [text, setText] = useState('');
  const [note, setNote] = useState<Note>(null);
  const [busy, setBusy] = useState(false);
  const textId = useId();
  const me = session?.user.id;
  const admin = session?.user.role === 'rcon_admin';
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const mayDelete = (c: MessageComment) => admin || mine || (!!c.author.id && c.author.id === me);
  // The RCON admin receives no message to server admins, moderators or a
  // scope; there only the sender comments. The backend decides (D-085).
  const mayComment = mine || !admin || !notToAdmin.includes(m.audience);

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
    <section
      aria-label={t('messages.commentsLabel')}
      className="flex flex-col gap-2 border-t border-border pt-3"
    >
      <h2 className="font-semibold">{t('messages.commentsLabel')}</h2>
      {list.isError && (
        <p role="alert" className="text-danger wrap-anywhere">
          {failText(t, list.error)}
        </p>
      )}
      {items.length > 0 && (
        <ol className="flex flex-col gap-2 border-l-2 border-border pl-3">
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
      {mayComment && (
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
      )}
    </section>
  );
}
