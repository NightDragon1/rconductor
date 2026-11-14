// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import logoLight from '@/assets/logo/logo.svg';
import logoDark from '@/assets/logo/logo-dark.svg';
import markDark from '@/assets/logo/mark.svg';
import markLight from '@/assets/logo/mark-light.svg';
import { cn } from '@/lib/utils';

// Logo shows the project logo (UI-023), made by scripts/logo.sh from
// assets/logo. The word mark and the mark alone come in a variant per
// design, so the gradient keeps 3:1 on the surfaces of each (UI-001);
// index.css shows the one of the current design.
export function Logo({ className, markOnly = false }: { className?: string; markOnly?: boolean }) {
  const { t } = useTranslation();
  if (markOnly) {
    return (
      <>
        <img src={markLight} alt={t('app.mark')} className={cn('logo-light size-9', className)} />
        <img src={markDark} alt={t('app.mark')} className={cn('logo-dark size-9', className)} />
      </>
    );
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
