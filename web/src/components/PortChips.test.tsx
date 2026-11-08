// SPDX-License-Identifier: AGPL-3.0-or-later
import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Server } from '@/api/servers';
import { portsOf } from '@/lib/ports';
import { setLanguage } from '@/test/render';
import { PortChips } from './PortChips';

const base: Server = {
  id: 's1',
  name: 'mc',
  groups: [],
  status: 'connected',
  status_since: '',
  version: 'v',
};

describe('port chips (UI-005)', () => {
  it('show the host port, mark unpublished ones and link Steam', () => {
    setLanguage('en');
    render(
      <PortChips
        label="Ports"
        ports={[
          {
            kind: 'game',
            name: 'game',
            private: 25565,
            public: 30000,
            proto: 'tcp',
            steam_url: 'steam://connect/mc.example:30000',
          },
          { kind: 'game', name: 'voice', private: 24454, proto: 'udp' },
          { kind: 'rcon', private: 25575, public: 25575, proto: 'tcp' },
          { kind: 'other', private: 8123, public: 8123, proto: 'tcp' },
        ]}
      />,
    );
    const items = within(screen.getByRole('list', { name: 'Ports' })).getAllByRole('listitem');
    expect(items.map((i) => i.textContent)).toEqual([
      'game30000/tcpConnect',
      'voice24454/udpnot published',
      'RCON25575/tcp',
      'port8123/tcp',
    ]);
    expect(screen.getByRole('link', { name: 'Connect' }).getAttribute('href')).toBe(
      'steam://connect/mc.example:30000',
    );
  });

  it('take the configured game ports of a server without agent', () => {
    const s: Server = { ...base, game_ports: [{ port: 25565, proto: 'tcp', name: 'game' }] };
    expect(portsOf(s)).toEqual([{ kind: 'game', name: 'game', private: 25565, proto: 'tcp' }]);
    expect(portsOf(base)).toEqual([]);
  });
});
