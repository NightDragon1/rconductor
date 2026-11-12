// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { Link, Outlet, useNavigate } from '@tanstack/react-router';
import { LiveProvider } from '@/lib/live-context';
import { UndoProvider } from '@/lib/undo';
import {
  ChevronsLeft,
  ChevronsRight,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Sun,
  Terminal,
  type LucideIcon,
} from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '@/api/client';
import { useSession } from '@/api/session';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from '@/components/ui/sheet';
import { bufferOwner } from '@/lib/console';
import { cn } from '@/lib/utils';
import { applyTheme, storedTheme, themes, type Theme } from '@/lib/theme';
import { adminNav, mainNav, type NavEntry } from '@/nav';

const collapsedKey = 'rcd.nav.collapsed';

// themeIcon shows the chosen design on its switch (UI-001).
const themeIcon: Record<Theme, LucideIcon> = { auto: Monitor, light: Sun, dark: Moon };

function storedCollapsed(): boolean {
  try {
    return localStorage.getItem(collapsedKey) === '1';
  } catch {
    return false;
  }
}

// Shell is the frame of every page with a session (UI-011): a sidebar
// with the entries of the role, collapsible on wide screens and a menu
// on phones (Design 6.1, 6.2).
export function Shell() {
  const { t } = useTranslation();
  const { data: account } = useSession();
  bufferOwner(account?.user.id);
  const [collapsed, setCollapsed] = useState(storedCollapsed);
  const [menuOpen, setMenuOpen] = useState(false);

  function toggle() {
    const next = !collapsed;
    setCollapsed(next);
    try {
      localStorage.setItem(collapsedKey, next ? '1' : '0');
    } catch {
      // the choice lasts for this page only
    }
  }

  return (
    <div className="flex min-h-full">
      <aside
        className={cn(
          'sticky top-0 hidden h-screen shrink-0 flex-col border-r border-border bg-surface md:flex',
          collapsed ? 'w-16' : 'w-64',
        )}
      >
        <Brand collapsed={collapsed} />
        <Navigation collapsed={collapsed} />
        <Button
          variant="ghost"
          className={cn('mx-2 mb-2', collapsed ? 'px-0' : 'justify-start')}
          onClick={toggle}
          aria-label={t(collapsed ? 'nav.expand' : 'nav.collapse')}
          title={t(collapsed ? 'nav.expand' : 'nav.collapse')}
        >
          {collapsed ? <ChevronsRight aria-hidden /> : <ChevronsLeft aria-hidden />}
          {!collapsed && <span>{t('nav.collapse')}</span>}
        </Button>
        <UserBlock collapsed={collapsed} />
        <PanelVersion collapsed={collapsed} />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-2 border-b border-border bg-surface px-2 md:hidden">
          <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" aria-label={t('nav.menu')}>
                <Menu aria-hidden />
              </Button>
            </SheetTrigger>
            <SheetContent aria-describedby={undefined}>
              <SheetTitle className="sr-only">{t('nav.menu')}</SheetTitle>
              <Brand collapsed={false} />
              <Navigation collapsed={false} onNavigate={() => setMenuOpen(false)} />
              <UserBlock collapsed={false} />
              <PanelVersion collapsed={false} />
            </SheetContent>
          </Sheet>
          <span className="font-semibold">{t('app.name')}</span>
          <ProfileLink />
        </header>
        <main className="min-w-0 flex-1 p-4 md:p-8">
          <LiveProvider key={account?.user.id}>
            <UndoProvider>
              <Outlet />
            </UndoProvider>
          </LiveProvider>
        </main>
      </div>
    </div>
  );
}

function Brand({ collapsed }: { collapsed: boolean }) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-3 px-4 py-5">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-accent/60 bg-accent/10 text-accent">
        <Terminal aria-hidden className="size-5" />
      </span>
      {!collapsed && (
        <span className="leading-tight">
          <span className="block font-semibold">{t('app.name')}</span>
          <span className="block text-xs text-muted">{t('app.org')}</span>
        </span>
      )}
    </div>
  );
}

function Navigation({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  if (!session) return null;
  const ctx = {
    supportersEnabled: session.ui.supporters_enabled,
    supportersAllowed: session.ui.supporters_allowed,
  };
  const visible = (list: NavEntry[]) => list.filter((e) => e.allowed(session.user.role, ctx));
  const admin = visible(adminNav);
  const item = (e: NavEntry) => (
    <li key={e.to}>
      <Link
        to={e.to}
        onClick={onNavigate}
        activeOptions={{ exact: e.to === '/' }}
        className={cn(
          'flex min-h-11 items-center gap-3 rounded-md px-3 text-sm text-muted hover:bg-surface-raised hover:text-text',
          collapsed && 'justify-center px-0',
        )}
        activeProps={{ className: 'bg-surface-raised text-text', 'aria-current': 'page' }}
        title={collapsed ? t(`nav.${e.key}`) : undefined}
      >
        <e.icon aria-hidden className="size-5 shrink-0" />
        {collapsed ? (
          <span className="sr-only">{t(`nav.${e.key}`)}</span>
        ) : (
          <span>{t(`nav.${e.key}`)}</span>
        )}
      </Link>
    </li>
  );
  return (
    <nav aria-label={t('nav.label')} className="flex-1 overflow-y-auto px-2">
      <ul className="space-y-1">{visible(mainNav).map(item)}</ul>
      {admin.length > 0 && (
        <>
          {collapsed ? (
            <hr className="my-3 border-border" />
          ) : (
            <h2 className="mt-6 mb-2 px-3 text-xs font-semibold tracking-wide text-muted uppercase">
              {t('nav.admin')}
            </h2>
          )}
          <ul className="space-y-1">{admin.map(item)}</ul>
        </>
      )}
    </nav>
  );
}

// ProfileLink is the avatar in the head of a phone; signing out stays in
// the menu.
function ProfileLink() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  if (!session) return null;
  return (
    <Link
      to="/profile"
      aria-label={t('nav.profile')}
      className="ml-auto flex size-11 items-center justify-center"
    >
      <Initials name={session.user.pseudonym} />
    </Link>
  );
}

function Initials({ name }: { name: string }) {
  return (
    <span
      aria-hidden
      className="flex size-9 shrink-0 items-center justify-center rounded-full bg-accent/20 text-xs font-semibold text-accent"
    >
      {name.slice(0, 2).toUpperCase()}
    </span>
  );
}

// PanelVersion is the small line below the user block in the sidebar and
// the phone menu: every role sees which panel version runs (UI-015).
export function PanelVersion({ collapsed }: { collapsed: boolean }) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  if (!session) return null;
  const text = t('app.version', { version: session.panel_version });
  return (
    <p
      className={cn(
        'px-3 pb-3 text-xs text-muted',
        collapsed ? 'truncate text-center' : 'wrap-anywhere',
      )}
      title={text}
    >
      {collapsed ? session.panel_version : text}
    </p>
  );
}

function UserBlock({ collapsed }: { collapsed: boolean }) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [theme, setTheme] = useState(storedTheme);
  const [logoutFailed, setLogoutFailed] = useState(false);
  if (!session) return null;
  const name = session.user.pseudonym;
  const nextTheme = themes[(themes.indexOf(theme) + 1) % themes.length] ?? 'auto';
  const themeLabel = t('nav.theme', { design: t(`nav.design.${theme}`) });
  const ThemeIcon = themeIcon[theme];

  async function logout() {
    const r = await api.POST('/auth/logout');
    // 401: the session is gone already.
    if (!r.response.ok && r.response.status !== 401) {
      setLogoutFailed(true);
      return;
    }
    qc.clear();
    bufferOwner(undefined);
    await navigate({ to: '/login' });
  }

  return (
    <div
      className={cn('flex items-center gap-2 border-t border-border p-3', collapsed && 'flex-col')}
    >
      <Link
        to="/profile"
        className="flex min-h-11 min-w-0 flex-1 items-center gap-3"
        title={t('nav.profile')}
      >
        <Initials name={name} />
        {collapsed ? (
          <span className="sr-only">{t('nav.profile')}</span>
        ) : (
          <span className="min-w-0 leading-tight">
            <span className="block wrap-anywhere md:truncate text-sm font-medium">{name}</span>
            <span className="block text-xs text-muted">{t(`role.${session.user.role}`)}</span>
          </span>
        )}
      </Link>
      <Button
        variant="ghost"
        size="icon"
        aria-label={themeLabel}
        title={themeLabel}
        onClick={() => {
          applyTheme(nextTheme);
          setTheme(nextTheme);
        }}
      >
        <ThemeIcon aria-hidden data-design={theme} />
      </Button>
      {logoutFailed && (
        <p role="alert" className="text-xs text-danger">
          {t('nav.logoutFailed')}
        </p>
      )}
      <Button
        variant="ghost"
        size="icon"
        aria-label={t('nav.logout')}
        title={t('nav.logout')}
        onClick={logout}
      >
        <LogOut aria-hidden />
      </Button>
    </div>
  );
}
