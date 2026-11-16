// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useTranslation } from 'react-i18next';
import { useSession } from '@/api/session';

// SERVER_NUMBER_FLAG switches the server number on (UI-018).
export const SERVER_NUMBER_FLAG = 'server_number';

// useServerNumbers tells whether the flag server_number is on.
export function useServerNumbers(): boolean {
  const { data: session } = useSession();
  return (session?.ui.features ?? []).includes(SERVER_NUMBER_FLAG);
}

// ServerNumber is the badge of a server's number before its name, with the
// flag server_number only (UI-018).
export function ServerNumber({ number }: { number?: number }) {
  const { t } = useTranslation();
  const on = useServerNumbers();
  if (!on || !number) return null;
  return (
    <span
      className="mr-2 inline-block rounded border border-border px-1.5 align-middle font-mono text-xs font-normal text-muted"
      aria-label={t('servers.numberLabel', { number })}
    >
      #{number}
    </span>
  );
}
