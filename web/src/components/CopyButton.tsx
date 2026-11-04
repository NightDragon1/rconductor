// SPDX-License-Identifier: AGPL-3.0-or-later
import { useState, type RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { copyText } from '@/lib/clipboard';

// CopyButton copies text; outside a secure context it selects the shown
// text and asks for Ctrl+C.
export function CopyButton({
  text,
  target,
  label,
}: {
  text: string;
  target: RefObject<HTMLElement | null>;
  label: string;
}) {
  const { t } = useTranslation();
  const [state, setState] = useState<'idle' | 'copied' | 'selected'>('idle');
  return (
    <span className="inline-flex items-center">
      <Button
        variant="ghost"
        size="sm"
        className="max-md:flex-1"
        aria-label={label}
        title={label}
        onClick={() => void copyText(text, target.current).then(setState)}
      >
        {state === 'copied' ? t('copy.copied') : t('copy.copy')}
      </Button>
      <span role="status" className="ml-2 text-xs text-muted empty:ml-0">
        {state === 'selected' ? t('copy.pressCtrlC') : ''}
      </span>
    </span>
  );
}
