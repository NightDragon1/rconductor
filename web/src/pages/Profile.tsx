// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, apiBase, must } from '@/api/client';
import { useSession } from '@/api/session';
import { outcome } from '@/api/users';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';

type Message = { ok: boolean; text: string } | null;

function Note({ m }: { m: Message }) {
  if (!m) return null;
  return (
    <p
      role={m.ok ? 'status' : 'alert'}
      className={m.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
    >
      {m.text}
    </p>
  );
}

// ProfilePage is the own profile (AUTH-009): pseudonym (read only), real
// name, contacts, profile picture, password. Who sees what is decided by
// the backend.
export function ProfilePage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: session } = useSession();
  const me = useQuery({ queryKey: ['me'], queryFn: async () => must(await api.GET('/me')) });
  const [realName, setRealName] = useState<string | null>(null);
  const [contacts, setContacts] = useState<[string, string][] | null>(null);
  const [info, setInfo] = useState<Message>(null);
  const [pic, setPic] = useState<Message>(null);
  const [pw, setPw] = useState<Message>(null);
  const [avatarAt, setAvatarAt] = useState(() => Date.now());
  const ids = { name: useId(), file: useId(), cur: useId(), next: useId(), again: useId() };
  const rows = contacts ?? Object.entries(me.data?.contacts ?? {});
  const name = realName ?? me.data?.real_name ?? '';

  const saveInfo = async (e: FormEvent) => {
    e.preventDefault();
    const r = await outcome('me', async () =>
      must(
        await api.PATCH('/me', {
          body: {
            real_name: name.trim(),
            contacts: Object.fromEntries(
              rows.filter(([k, v]) => k.trim() && v.trim()).map(([k, v]) => [k.trim(), v.trim()]),
            ),
          },
        }),
      ),
    );
    setInfo(
      r.ok ? { ok: true, text: t('admin.saved') } : { ok: false, text: reasonText(t, r.reason) },
    );
    if (r.ok) {
      setRealName(null);
      setContacts(null);
      void qc.invalidateQueries({ queryKey: ['me'] });
    }
  };
  const upload = async (file: File) => {
    const r = await fetch(`${apiBase()}/me/avatar`, {
      method: 'PUT',
      body: file,
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
      credentials: 'same-origin',
    });
    setPic(
      r.ok
        ? { ok: true, text: t('profile.pictureSaved') }
        : { ok: false, text: t('profile.pictureRefused') },
    );
    setAvatarAt(Date.now());
    void qc.invalidateQueries({ queryKey: ['me'] });
  };
  const removePic = async () => {
    const r = await outcome('avatar', async () => must(await api.DELETE('/me/avatar')));
    setPic(
      r.ok
        ? { ok: true, text: t('profile.pictureRemoved') }
        : { ok: false, text: reasonText(t, r.reason) },
    );
    void qc.invalidateQueries({ queryKey: ['me'] });
  };
  const changePw = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const next = String(f.get('next'));
    if (next.length < 12 || next.length > 128)
      return setPw({ ok: false, text: t('password.length') });
    if (next !== String(f.get('again'))) return setPw({ ok: false, text: t('password.mismatch') });
    const r = await api.POST('/me/password', {
      body: { current_password: String(f.get('current')), new_password: next },
    });
    setPw(
      r.response.ok
        ? { ok: true, text: t('profile.passwordChanged') }
        : { ok: false, text: t('password.failed') },
    );
    if (r.response.ok) e.currentTarget.reset();
  };

  return (
    <div className="flex max-w-2xl flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">{t('nav.profile')}</h1>
        <p className="text-sm text-muted">{t('profile.explain')}</p>
      </div>

      <form
        onSubmit={saveInfo}
        aria-label={t('profile.about')}
        className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
      >
        <h2 className="font-semibold">{t('profile.about')}</h2>
        <p className="text-sm">
          <span className="text-muted">{t('login.pseudonym')}</span>{' '}
          <bdi className="font-mono">{session?.user.pseudonym}</bdi>
        </p>
        <label htmlFor={ids.name} className="text-sm text-muted">
          {t('admin.realName')}
        </label>
        <Input
          id={ids.name}
          value={name}
          maxLength={128}
          onChange={(e) => setRealName(e.target.value)}
        />
        <h3 className="text-sm text-muted">{t('profile.contacts')}</h3>
        {rows.map(([k, v], i) => (
          <div key={i} className="grid grid-cols-[10rem_1fr_auto] gap-2">
            <Input
              aria-label={t('profile.contactKind')}
              value={k}
              list="contact-kinds"
              onChange={(e) =>
                setContacts(rows.map((x, j) => (j === i ? [e.target.value, x[1]] : x)))
              }
            />
            <Input
              aria-label={t('profile.contactValue')}
              value={v}
              onChange={(e) =>
                setContacts(rows.map((x, j) => (j === i ? [x[0], e.target.value] : x)))
              }
            />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t('admin.removeItem', { name: k })}
              onClick={() => setContacts(rows.filter((_, j) => j !== i))}
            >
              <X aria-hidden className="size-4" />
            </Button>
          </div>
        ))}
        <datalist id="contact-kinds">
          <option value="telegram" />
          <option value="whatsapp" />
          <option value="discord" />
        </datalist>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="self-start"
          onClick={() => setContacts([...rows, ['', '']])}
        >
          {t('profile.addContact')}
        </Button>
        <Note m={info} />
        <Button type="submit" className="self-start">
          {t('servers.save')}
        </Button>
      </form>

      <section
        aria-label={t('profile.picture')}
        className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
      >
        <h2 className="font-semibold">{t('profile.picture')}</h2>
        {me.data?.has_avatar && session && (
          <img
            src={`${apiBase()}/users/${session.user.id}/avatar?v=${avatarAt}`}
            alt={t('profile.pictureAlt')}
            className="size-24 rounded-full object-cover"
          />
        )}
        <label htmlFor={ids.file} className="text-sm text-muted">
          {t('profile.choosePicture')}
        </label>
        <input
          id={ids.file}
          type="file"
          className="min-h-11"
          accept="image/png,image/jpeg,image/webp"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void upload(f);
          }}
        />
        {me.data?.has_avatar && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="self-start"
            onClick={() => void removePic()}
          >
            {t('profile.removePicture')}
          </Button>
        )}
        <Note m={pic} />
      </section>

      <form
        onSubmit={changePw}
        aria-label={t('password.submit')}
        className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
      >
        <h2 className="font-semibold">{t('password.submit')}</h2>
        <p className="text-sm text-muted">{t('profile.passwordHint')}</p>
        <label htmlFor={ids.cur} className="text-sm text-muted">
          {t('password.current')}
        </label>
        <Input
          id={ids.cur}
          name="current"
          type="password"
          autoComplete="current-password"
          required
        />
        <label htmlFor={ids.next} className="text-sm text-muted">
          {t('password.new')}
        </label>
        <Input id={ids.next} name="next" type="password" autoComplete="new-password" required />
        <label htmlFor={ids.again} className="text-sm text-muted">
          {t('password.repeat')}
        </label>
        <Input id={ids.again} name="again" type="password" autoComplete="new-password" required />
        <Note m={pw} />
        <Button type="submit" className="self-start">
          {t('password.submit')}
        </Button>
      </form>
    </div>
  );
}
