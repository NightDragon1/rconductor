// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useSession } from '@/api/session';
import { ServerBoard } from '@/components/ServerBoard';
import { navContext, serverViews, shown } from '@/nav';
import { ServersAdminPage } from '@/pages/admin/ServersAdmin';

// ServerViews are the tabs of the main entry Servers (Design 6.2): a tab
// the role may not open is hidden; the route and the backend check anyway.
function ServerViews({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const tabs = session
    ? serverViews.filter((e) => shown(e, session.user.role, navContext(session.ui)))
    : [];
  return (
    <div className="flex flex-col gap-5">
      {tabs.length > 1 && (
        <nav
          aria-label={t('servers.tabs')}
          data-scrolls
          className="flex gap-1 overflow-x-auto border-b border-border"
        >
          {tabs.map((e) => (
            <Link
              key={e.to}
              to={e.to}
              activeOptions={{ exact: true }}
              className="-mb-px border-b-2 border-transparent px-4 py-2.5 whitespace-nowrap text-muted hover:text-text"
              activeProps={{
                className: 'border-accent! text-text! font-semibold',
                'aria-current': 'page',
              }}
            >
              {t(`servers.tab.${e.key}`)}
            </Link>
          ))}
        </nav>
      )}
      {children}
    </div>
  );
}

// ServersPage lists the servers in scope as cards with a search (F13).
export function ServersPage() {
  const { t } = useTranslation();
  return (
    <ServerViews>
      <ServerBoard title={t('nav.servers')} description={t('servers.explainCards')} />
    </ServerViews>
  );
}

// ServersManagePage is the management of servers and server groups as the
// second tab of Servers; the server admin reads it only, as before.
export function ServersManagePage() {
  return (
    <ServerViews>
      <ServersAdminPage />
    </ServerViews>
  );
}
