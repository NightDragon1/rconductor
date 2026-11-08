// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import { PasswordInput } from '@/components/PasswordInput';
import { TwoFactorSetup } from '@/components/TwoFactorSetup';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

const key = ['me', 'second-factor'] as const;

type Message = { ok: boolean; text: string } | null;

function errorText(t: (k: string) => string, e: unknown): string {
  const p = e instanceof ApiError ? e.problem : undefined;
  if (p?.reason === 'rate_limited') return t('twofactor.locked');
  if (p?.reason === 'conflict') return t('twofactor.requiredOn');
  if (p?.reason === 'invalid' && p.detail && p.detail !== 'password or code wrong') return p.detail;
  return t('twofactor.wrong');
}

// ProfileSecondFactor is the own second factor and the address for its
// recovery (AUTH-008). Switching off and a new address need the password
// and, with the second factor on, a code: a session alone must not lock
// its owner out.
export function ProfileSecondFactor() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const sf = useQuery({
    queryKey: key,
    queryFn: async () => must(await api.GET('/me/second-factor')),
  });
  const ids = { pw: useId(), code: useId(), mail: useId(), mailPw: useId(), mailCode: useId() };
  const [off, setOff] = useState<Message>(null);
  const [mail, setMail] = useState<Message>(null);
  const [done, setDone] = useState<Message>(null);
  if (!sf.data) return null;
  const on = sf.data.enabled;

  const disable = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    try {
      must(
        await api.POST('/me/second-factor/disable', {
          body: { password: String(f.get('password')), code: String(f.get('code')) },
        }),
      );
      setOff({ ok: true, text: t('twofactor.off') });
      await qc.invalidateQueries({ queryKey: key });
    } catch (err) {
      setOff({ ok: false, text: errorText(t, err) });
    }
  };
  const saveMail = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    try {
      must(
        await api.PUT('/me/email', {
          body: {
            email: String(f.get('email')).trim(),
            password: String(f.get('password')),
            ...(on ? { code: String(f.get('code')) } : {}),
          },
        }),
      );
      setMail({ ok: true, text: t('admin.saved') });
      await qc.invalidateQueries({ queryKey: key });
    } catch (err) {
      setMail({ ok: false, text: errorText(t, err) });
    }
  };

  return (
    <>
      <section
        aria-label={t('twofactor.title')}
        className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
      >
        <h2 className="font-semibold">{t('twofactor.title')}</h2>
        <p className="text-sm">{t(on ? 'twofactor.isOn' : 'twofactor.isOff')}</p>
        {done && (
          <p role="status" className="text-sm text-ok">
            {done.text}
          </p>
        )}
        {!on && (
          <TwoFactorSetup
            onDone={() => {
              setDone({ ok: true, text: t('twofactor.nowOn') });
              void qc.invalidateQueries({ queryKey: key });
            }}
          />
        )}
        {on && sf.data.required && (
          <p className="text-sm text-muted">{t('twofactor.requiredOn')}</p>
        )}
        {on && !sf.data.required && (
          <form className="flex flex-col gap-3" onSubmit={(e) => void disable(e)}>
            <label htmlFor={ids.pw} className="text-sm text-muted">
              {t('twofactor.password')}
            </label>
            <PasswordInput id={ids.pw} name="password" autoComplete="current-password" required />
            <label htmlFor={ids.code} className="text-sm text-muted">
              {t('twofactor.code')}
            </label>
            <Input
              id={ids.code}
              name="code"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={16}
              required
            />
            {off && (
              <p
                role={off.ok ? 'status' : 'alert'}
                className={off.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
              >
                {off.text}
              </p>
            )}
            <Button
              type="submit"
              variant="outline"
              className="self-start border-danger text-danger"
            >
              {t('twofactor.switchOff')}
            </Button>
          </form>
        )}
      </section>
      <form
        aria-label={t('twofactor.mailTitle')}
        onSubmit={(e) => void saveMail(e)}
        className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4"
      >
        <h2 className="font-semibold">{t('twofactor.mailTitle')}</h2>
        <p className="text-sm text-muted">{t('twofactor.mailExplain')}</p>
        <label htmlFor={ids.mail} className="text-sm text-muted">
          {t('twofactor.mail')}
        </label>
        <Input
          id={ids.mail}
          name="email"
          type="email"
          maxLength={254}
          defaultValue={sf.data.email}
        />
        <label htmlFor={ids.mailPw} className="text-sm text-muted">
          {t('twofactor.password')}
        </label>
        <PasswordInput id={ids.mailPw} name="password" autoComplete="current-password" required />
        {on && (
          <>
            <label htmlFor={ids.mailCode} className="text-sm text-muted">
              {t('twofactor.code')}
            </label>
            <Input
              id={ids.mailCode}
              name="code"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={16}
              required
            />
          </>
        )}
        {mail && (
          <p
            role={mail.ok ? 'status' : 'alert'}
            className={mail.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
          >
            {mail.text}
          </p>
        )}
        <Button type="submit" className="self-start">
          {t('admin.save')}
        </Button>
      </form>
    </>
  );
}
