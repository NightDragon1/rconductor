// SPDX-License-Identifier: AGPL-3.0-or-later

// The pages of the interface per test user (Design 6.2), for checks that
// walk through all of them.
export const pages: Record<string, string[]> = {
  admin: [
    '',
    'servers',
    'audit',
    'profile',
    'admin/users',
    'admin/groups',
    'admin/command-lists',
    'admin/servers',
    'admin/agents',
    'admin/settings',
  ],
  sadmin: ['', 'admin/users', 'admin/groups', 'admin/command-lists', 'admin/servers'],
  mod: ['', 'servers', 'admin/users'],
  sup: ['', 'servers'],
};

export const serverTabs = ['', '/console', '/commands', '/logs', '/history'];
