// SPDX-License-Identifier: AGPL-3.0-or-later
import {
  Bot,
  FileClock,
  LayoutGrid,
  ListChecks,
  Server,
  Settings,
  ShieldCheck,
  UsersRound,
  type LucideIcon,
} from 'lucide-react';
import type { components } from '@/api/schema';

export type Role = components['schemas']['Role'];

export interface NavEntry {
  to: string;
  key: string; // i18n key below nav.
  icon: LucideIcon;
  // allowed decides per role (Design 6.2); entries without right are
  // hidden, not greyed. The backend checks every request anyway.
  allowed: (role: Role, me: { supportersAllowed: boolean }) => boolean;
}

const admins = (r: Role) => r === 'rcon_admin' || r === 'server_admin';
const rconAdmin = (r: Role) => r === 'rcon_admin';

export const mainNav: NavEntry[] = [
  { to: '/', key: 'overview', icon: LayoutGrid, allowed: () => true },
  { to: '/servers', key: 'servers', icon: Server, allowed: () => true },
  { to: '/audit', key: 'audit', icon: FileClock, allowed: admins },
];

export const adminNav: NavEntry[] = [
  {
    to: '/admin/users',
    key: 'users',
    icon: UsersRound,
    allowed: (r, me) => admins(r) || (r === 'moderator' && me.supportersAllowed),
  },
  { to: '/admin/groups', key: 'groups', icon: ShieldCheck, allowed: admins },
  { to: '/admin/command-lists', key: 'commandLists', icon: ListChecks, allowed: admins },
  { to: '/admin/servers', key: 'serverAdmin', icon: Server, allowed: admins },
  { to: '/admin/agents', key: 'agents', icon: Bot, allowed: rconAdmin },
  { to: '/admin/settings', key: 'settings', icon: Settings, allowed: rconAdmin },
];

export function entryFor(path: string): NavEntry | undefined {
  return [...mainNav, ...adminNav].find((e) => e.to === path);
}
