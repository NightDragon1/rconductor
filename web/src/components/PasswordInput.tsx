// SPDX-License-Identifier: AGPL-3.0-or-later
import { Eye, EyeOff } from 'lucide-react';
import { useEffect, useRef, useState, type InputHTMLAttributes } from 'react';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';

// SHOW_MS is how long a password stays visible (UI-013).
export const SHOW_MS = 10_000;

// PasswordInput can show its value for a moment; it is hidden again after
// SHOW_MS and before the form goes out, so that the browser never takes it
// for plain text.
export function PasswordInput(props: InputHTMLAttributes<HTMLInputElement>) {
  const { t } = useTranslation();
  const [shown, setShown] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!shown) return;
    const form = input.current?.form;
    const hide = () => setShown(false);
    const timer = setTimeout(hide, SHOW_MS);
    form?.addEventListener('submit', hide);
    return () => {
      clearTimeout(timer);
      form?.removeEventListener('submit', hide);
    };
  }, [shown]);
  return (
    <div className="relative">
      <Input {...props} ref={input} type={shown ? 'text' : 'password'} className="pr-12" />
      <button
        type="button"
        className="tap absolute top-0 right-0 justify-center text-muted hover:text-text"
        aria-label={t(shown ? 'password.hide' : 'password.show')}
        aria-pressed={shown}
        onClick={() => setShown((s) => !s)}
      >
        {shown ? <EyeOff aria-hidden className="size-5" /> : <Eye aria-hidden className="size-5" />}
      </button>
    </div>
  );
}
