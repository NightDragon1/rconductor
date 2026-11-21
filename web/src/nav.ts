// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import {
  BookOpen,
  Bot,
  FileClock,
  LayoutGrid,
  ListChecks,
  Mail,
  Network,
  ScrollText,
  Server,
  Settings,
  ShieldCheck,
  UsersRound,
  type LucideIcon,
} from 'lucide-react';
import type { components } from '@/api/schema';

export type Role = components['schemas']['Role'];

// NavContext holds what decides beyond the role: a moderator manages
// supporters only with the global switch and his own release (Design 6.2,
// ROLE-006).
export interface NavContext {
  supportersEnabled: boolean;
  supportersAllowed: boolean;
  features: readonly string[]; // flags that are on (NFR-019)
}

export interface NavEntry {
  to: string;
  key: string; // i18n key below nav.
  icon: LucideIcon;
  // allowed decides per role (Design 6.2); entries without right are
  // hidden, not greyed. The backend checks every request anyway.
  allowed: (role: Role, me: NavContext) => boolean;
  feature?: string; // while this flag is off, entry and page do not exist
}

// navContext takes what the entries need from the session.
export function navContext(ui: components['schemas']['Session']['ui']): NavContext {
  return {
    supportersEnabled: ui.supporters_enabled,
    supportersAllowed: ui.supporters_allowed,
    features: ui.features ?? [],
  };
}

// shown decides by the entry's flag, if any, and its rule.
export function shown(e: NavEntry, role: Role, me: NavContext): boolean {
  return (e.feature === undefined || me.features.includes(e.feature)) && e.allowed(role, me);
}

const admins = (r: Role) => r === 'rcon_admin' || r === 'server_admin';
const rconAdmin = (r: Role) => r === 'rcon_admin';

export const mainNav: NavEntry[] = [
  { to: '/', key: 'overview', icon: LayoutGrid, allowed: () => true },
  { to: '/servers', key: 'servers', icon: Server, allowed: () => true },
  { to: '/topology', key: 'topology', icon: Network, allowed: admins },
  { to: '/audit', key: 'audit', icon: FileClock, allowed: admins },
  // Read only for every role (UI-020); the backend shows each role what it may see.
  {
    to: '/profiles',
    key: 'profiles',
    icon: BookOpen,
    allowed: () => true,
    feature: 'profile_browser',
  },
  // Every role receives messages (MSG-001 to MSG-003); the backend shows
  // each role its own.
  { to: '/messages', key: 'messages', icon: Mail, allowed: () => true, feature: 'messages' },
];

export const adminNav: NavEntry[] = [
  {
    to: '/admin/users',
    key: 'users',
    icon: UsersRound,
    allowed: (r, me) =>
      admins(r) || (r === 'moderator' && me.supportersEnabled && me.supportersAllowed),
  },
  { to: '/admin/groups', key: 'groups', icon: ShieldCheck, allowed: admins },
  { to: '/admin/command-lists', key: 'commandLists', icon: ListChecks, allowed: admins },
  { to: '/admin/agents', key: 'agents', icon: Bot, allowed: rconAdmin },
  { to: '/admin/settings', key: 'settings', icon: Settings, allowed: rconAdmin },
  // System log of the panel and of the agents (UI-025, D-091).
  {
    to: '/admin/system-log',
    key: 'systemLog',
    icon: ScrollText,
    allowed: rconAdmin,
    feature: 'diagnostics_export',
  },
];

// The tabs of the main entry Servers: the cards for every role, the
// management of servers and server groups for the admins, read only for
// the server admin as before (Design 6.2). The keys sit below servers.tab.
export const serverViews: NavEntry[] = [
  { to: '/servers', key: 'cards', icon: LayoutGrid, allowed: () => true },
  { to: '/servers/manage', key: 'manage', icon: Server, allowed: admins },
];

export function entryFor(path: string): NavEntry | undefined {
  return [...mainNav, ...adminNav, ...serverViews].find((e) => e.to === path);
}
