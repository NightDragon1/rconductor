// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import logoLight from '@/assets/logo/logo.svg';
import logoDark from '@/assets/logo/logo-dark.svg';
import mark from '@/assets/logo/mark.svg';
import { cn } from '@/lib/utils';

// Logo shows the project logo (UI-023), made by scripts/logo.sh from
// assets/logo. The word mark comes in a variant per design; index.css
// shows the one of the current design (UI-001). The mark alone reads on
// both designs.
export function Logo({ className, markOnly = false }: { className?: string; markOnly?: boolean }) {
  const { t } = useTranslation();
  if (markOnly) {
    return <img src={mark} alt={t('app.mark')} className={cn('size-9', className)} />;
  }
  return (
    <>
      <img
        src={logoLight}
        alt={t('app.logo')}
        className={cn('logo-light h-12 w-auto', className)}
      />
      <img src={logoDark} alt={t('app.logo')} className={cn('logo-dark h-12 w-auto', className)} />
    </>
  );
}
