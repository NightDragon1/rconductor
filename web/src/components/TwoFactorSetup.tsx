// SPDX-License-Identifier: AGPL-3.0-or-later
import QRCode from 'qrcode';
import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, must } from '@/api/client';
import { CopyButton } from '@/components/CopyButton';
import { PasswordInput } from '@/components/PasswordInput';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

type Setup = { secret: string; uri: string };

// problemText names a failed step without telling more than the backend.
function useProblemText() {
  const { t } = useTranslation();
  return (e: unknown) => {
    const reason = e instanceof ApiError ? e.problem?.reason : undefined;
    if (reason === 'rate_limited') return t('twofactor.locked');
    if (reason === 'conflict') return t('twofactor.alreadyOn');
    return t('twofactor.wrong');
  };
}

// TwoFactorSetup sets up the second factor (AUTH-008): the password first,
// then the QR code, made in the browser without any outside service, with
// secret and otpauth URI to type in, and the first code that switches it
// on.
export function TwoFactorSetup({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const problem = useProblemText();
  const ids = { pw: useId(), code: useId() };
  const [setup, setSetup] = useState<Setup | null>(null);
  const secretRef = useRef<HTMLElement>(null);
  const uriRef = useRef<HTMLElement>(null);
  const [qr, setQr] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!setup) return;
    let live = true;
    void QRCode.toDataURL(setup.uri, { errorCorrectionLevel: 'M', margin: 1, width: 192 }).then(
      (url) => live && setQr(url),
    );
    return () => {
      live = false;
    };
  }, [setup]);

  const start = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError('');
    try {
      setSetup(
        must(
          await api.POST('/me/second-factor/setup', {
            body: { password: String(f.get('password')) },
          }),
        ),
      );
    } catch (err) {
      setError(problem(err));
    } finally {
      setBusy(false);
    }
  };

  const confirm = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    setError('');
    try {
      must(await api.POST('/me/second-factor/confirm', { body: { code: String(f.get('code')) } }));
      onDone();
    } catch (err) {
      setError(problem(err));
    } finally {
      setBusy(false);
    }
  };

  if (!setup) {
    return (
      <form className="flex flex-col gap-3" onSubmit={(e) => void start(e)}>
        <p className="text-sm text-muted">{t('twofactor.setupExplain')}</p>
        <label htmlFor={ids.pw} className="text-sm">
          {t('twofactor.password')}
        </label>
        <PasswordInput id={ids.pw} name="password" autoComplete="current-password" required />
        {error && (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        )}
        <Button type="submit" className="self-start" disabled={busy}>
          {t('twofactor.start')}
        </Button>
      </form>
    );
  }
  return (
    <form className="flex flex-col gap-3" onSubmit={(e) => void confirm(e)}>
      <p className="text-sm text-muted">{t('twofactor.scan')}</p>
      {qr && (
        <img
          src={qr}
          alt={t('twofactor.qr')}
          width={192}
          height={192}
          className="rounded-md bg-white p-2"
        />
      )}
      <div className="flex flex-col gap-1 text-sm">
        <span className="text-muted">{t('twofactor.secret')}</span>
        <span className="flex items-center gap-2">
          <code ref={secretRef} className="font-mono break-all">
            {setup.secret}
          </code>
          <CopyButton text={setup.secret} target={secretRef} label={t('twofactor.copySecret')} />
        </span>
      </div>
      <div className="flex flex-col gap-1 text-sm">
        <span className="text-muted">{t('twofactor.uri')}</span>
        <span className="flex items-center gap-2">
          <code ref={uriRef} className="font-mono text-xs break-all">
            {setup.uri}
          </code>
          <CopyButton text={setup.uri} target={uriRef} label={t('twofactor.copyUri')} />
        </span>
      </div>
      <label htmlFor={ids.code} className="text-sm">
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
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      )}
      <Button type="submit" className="self-start" disabled={busy}>
        {t('twofactor.confirm')}
      </Button>
    </form>
  );
}
