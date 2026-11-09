// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { agentsKey, profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { AgentsPage } from './Agents';
import { ServersAdminPage } from './ServersAdmin';

const now = new Date().toISOString();
const data: [readonly unknown[], unknown][] = [
  [
    sessionKey,
    {
      user: { id: 'a', pseudonym: 'boss', role: 'rcon_admin' },
      expires_at: now,
      must_change_password: false,
      ui: { shortcuts_enabled: false, supporters_enabled: true, supporters_allowed: false },
    },
  ],
  [
    serversKey,
    [
      {
        id: 's1',
        name: 'mc',
        groups: ['g1'],
        status: 'connected',
        status_since: now,
        version: 'v',
        profile_ids: ['p1'],
        host: 'mc.lan',
        port: 25575,
        credentials: 'reenter',
      },
    ],
  ],
  [serverGroupsKey, [{ id: 'g1', name: 'main', server_ids: ['s1'], version: 'v' }]],
  [profilesKey, [{ id: 'p1', name: 'Minecraft' }]],
  [
    agentsKey,
    [
      {
        id: 'a1',
        name: 'host-1',
        fingerprint: 'SHA256:abc',
        status: 'active',
        version: '1.0.0',
        capabilities: [],
        connected: true,
        created_at: now,
        last_seen_at: now,
      },
      {
        id: 'a2',
        name: 'host-3',
        fingerprint: 'SHA256:def',
        status: 'pending',
        version: '1.0.0',
        capabilities: [],
        connected: false,
        created_at: now,
      },
    ],
  ],
  [['settings'], { values: { 'agents.fingerprint_pairing': false }, etag: '"e"' }],
];
// Data, not texts: names, addresses, versions and fingerprints.
const isData =
  /^(mc|main|Minecraft|mc\.lan:25575|host-1|host-3|1\.0\.0|SHA256:abc|SHA256:def|aria-label=.*|title=SHA256:.*)$/;

describe('servers and agents', () => {
  it('servers page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(ServersAdminPage, data);
    fireEvent.click(screen.getByRole('button', { name: 'mc' }));
    expect(unmarked(container).filter((s) => !isData.test(s))).toEqual([]);
  });

  it('agents page has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(AgentsPage, data);
    expect(unmarked(container).filter((s) => !isData.test(s))).toEqual([]);
  });
});
