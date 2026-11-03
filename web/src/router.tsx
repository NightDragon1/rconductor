// SPDX-License-Identifier: AGPL-3.0-or-later
import type { QueryClient } from '@tanstack/react-query';
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  notFound,
  redirect,
} from '@tanstack/react-router';
import { fetchSession, sessionKey } from '@/api/session';
import { entryFor } from '@/nav';
import { placeholder } from '@/pages/Placeholder';
import { ChangePasswordPage } from '@/pages/ChangePassword';
import { HomePage } from '@/pages/Home';
import { serversQuery } from '@/api/servers';
import { defaultTab, tabsFor, type Tab } from '@/lib/serverpage';
import { CommandsTab } from '@/pages/server/CommandsTab';
import { ConsoleTab } from '@/pages/server/ConsoleTab';
import { HistoryTab } from '@/pages/server/HistoryTab';
import { LogsTab } from '@/pages/server/LogsTab';
import { OverviewTab } from '@/pages/server/OverviewTab';
import { ServerLayout } from '@/pages/server/ServerLayout';
import { ServersPage } from '@/pages/Servers';
import { InvitePage } from '@/pages/Invite';
import { LoginPage } from '@/pages/Login';
import { NotFoundPage } from '@/pages/NotFound';
import { Shell } from '@/pages/Shell';

export interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFoundPage,
});

// The invitation is open without a session; the token comes in the
// fragment (revision 2.4 candidate 23).
const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'invite',
  component: InvitePage,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'login',
  component: LoginPage,
});

// Everything else needs a session; a forced password change allows only
// the change itself (AUTH-010).
const sessionRoute = (ctx: RouterContext) =>
  ctx.queryClient.fetchQuery({ queryKey: sessionKey, queryFn: fetchSession, staleTime: 60_000 });

const changePasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'change-password',
  beforeLoad: async ({ context }) => {
    if (!(await sessionRoute(context))) throw redirect({ to: '/login' });
  },
  component: ChangePasswordPage,
});

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  beforeLoad: async ({ context }) => {
    const s = await sessionRoute(context);
    if (!s) throw redirect({ to: '/login' });
    if (s.must_change_password) throw redirect({ to: '/change-password' });
  },
  component: Shell,
});

const homeRoute = createRoute({ getParentRoute: () => appRoute, path: '/', component: HomePage });

// A page outside the role's navigation does not exist for it (Design 6.2).
// The guard needs only the session: no other request goes out for a page
// the role may not see. The backend checks every request anyway.
function guarded(path: string, key: string) {
  return createRoute({
    getParentRoute: () => appRoute,
    path,
    beforeLoad: async ({ context }) => {
      const entry = entryFor(path);
      const s = await sessionRoute(context);
      if (!entry || !s) throw notFound();
      if (
        !entry.allowed(s.user.role, {
          supportersEnabled: s.ui.supporters_enabled,
          supportersAllowed: s.ui.supporters_allowed,
        })
      )
        throw notFound();
    },
    component: placeholder(key),
  });
}

const pages = [
  guarded('/audit', 'audit'),
  guarded('/admin/users', 'users'),
  guarded('/admin/groups', 'groups'),
  guarded('/admin/command-lists', 'commandLists'),
  guarded('/admin/servers', 'serverAdmin'),
  guarded('/admin/agents', 'agents'),
  guarded('/admin/settings', 'settings'),
];
const serversRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'servers',
  component: ServersPage,
});

// The server page (Design 6.3, 6.4): the loader reads the server from the
// list in scope; a server outside it does not exist (Design 3.1).
const serverRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'servers/$serverId',
  loader: async ({ context, params }) => {
    const list = await context.queryClient.ensureQueryData(serversQuery);
    if (!list.some((s) => s.id === params.serverId)) throw notFound();
  },
  component: ServerLayout,
});

const serverIndexRoute = createRoute({
  getParentRoute: () => serverRoute,
  path: '/',
  beforeLoad: async ({ context, params }) => {
    const s = await sessionRoute(context);
    throw redirect({
      to: `/servers/$serverId/${defaultTab(s?.user.role)}`,
      params: { serverId: params.serverId },
      replace: true,
    });
  },
});

// A tab the role may not see does not exist for it (Design 6.3): the
// guard needs only the session, so no other request goes out. Logs need
// an agent; without one the tab does not exist either (F11).
function serverTab(tab: Tab, component: () => React.JSX.Element | null) {
  return createRoute({
    getParentRoute: () => serverRoute,
    path: tab,
    beforeLoad: async ({ context, params }) => {
      const s = await sessionRoute(context);
      const role = s?.user.role;
      if (!tabsFor(role, true).includes(tab)) throw notFound();
      if (tab === 'logs') {
        const list = await context.queryClient.ensureQueryData(serversQuery);
        if (!list.find((x) => x.id === params.serverId)?.agent_id) throw notFound();
      }
    },
    component,
  });
}
const serverTabs = [
  serverTab('overview', OverviewTab),
  serverTab('console', ConsoleTab),
  serverTab('commands', CommandsTab),
  serverTab('logs', LogsTab),
  serverTab('history', HistoryTab),
];
const profileRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'profile',
  component: placeholder('profile'),
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  inviteRoute,
  changePasswordRoute,
  appRoute.addChildren([
    homeRoute,
    serversRoute,
    serverRoute.addChildren([serverIndexRoute, ...serverTabs]),
    profileRoute,
    ...pages,
  ]),
]);

// The base path comes from <base href> (RCD_BASE_PATH, issue #88).
export function basepath(): string {
  const p = new URL(document.baseURI).pathname.replace(/\/$/, '');
  return p === '' ? '/' : p;
}

export function makeRouter(queryClient: QueryClient) {
  return createRouter({ routeTree, basepath: basepath(), context: { queryClient } });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
