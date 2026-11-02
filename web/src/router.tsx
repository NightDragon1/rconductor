// SPDX-License-Identifier: AGPL-3.0-or-later
import type { QueryClient } from '@tanstack/react-query';
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from '@tanstack/react-router';
import { fetchSession, sessionKey } from '@/api/session';
import { ChangePasswordPage } from '@/pages/ChangePassword';
import { HomePage } from '@/pages/Home';
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

const routeTree = rootRoute.addChildren([
  loginRoute,
  changePasswordRoute,
  appRoute.addChildren([homeRoute]),
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
