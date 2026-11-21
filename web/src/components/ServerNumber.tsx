// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useTranslation } from 'react-i18next';

// ServerNumber is the badge of a server's number before its name (UI-018).
export function ServerNumber({ number }: { number?: number }) {
  const { t } = useTranslation();
  if (!number) return null;
  // The visible "#n" is hidden from screen readers, which read the label
  // instead: aria-label on a span without role is not allowed (ARIA 1.2).
  return (
    <span className="mr-2 inline-block rounded border border-border px-1.5 align-middle font-mono text-xs font-normal text-muted">
      <span aria-hidden>#{number}</span>
      <span className="sr-only">{t('servers.numberLabel', { number })}</span>
    </span>
  );
}
