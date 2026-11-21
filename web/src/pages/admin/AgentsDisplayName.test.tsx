// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { agentsKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage } from '@/test/render';
import { AgentsPage } from './Agents';

const now = new Date().toISOString();
const session = (features: string[]) => ({
  user: { id: 'u0', pseudonym: 'boss', role: 'rcon_admin' },
  expires_at: now,
  must_change_password: false,
  ui: { shortcuts_enabled: false, supporters_enabled: true, features },
});
const agents = [
  {
    id: 'a1',
    name: 'host-1',
    display_name: 'Rack <b>1</b>',
    fingerprint: 'SHA256:x',
    status: 'active',
    version: '1.2.0',
    capabilities: [],
    connected: true,
    created_at: now,
  },
];
const data = (features: string[]) =>
  [
    [sessionKey, session(features)],
    [agentsKey, agents],
    [['settings'], { values: {}, etag: '' }],
  ] as [readonly unknown[], unknown][];

afterEach(() => vi.restoreAllMocks());

// The RCON admin sets the display name of a host (UI-021, D-089); the
// backend's refusal shows, empty removes it, without the flag nothing shows.
describe('display name of a host', () => {
  it('shows it as text and sends an empty one as null', async () => {
    setLanguage('en');
    const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({
      data: agents[0],
      response: new Response(null, { status: 200 }),
    } as never);
    await renderPage(AgentsPage, data(['host_groups']));
    const row = screen.getByRole('row', { name: /host-1/ });
    expect(row.textContent).toContain('Rack <b>1</b>');
    expect(row.querySelector('b')).toBeNull();
    fireEvent.click(within(row).getByRole('button', { name: 'Display name' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: '  ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(patch).toHaveBeenCalledWith('/agents/{id}', {
        params: { path: { id: 'a1' } },
        body: { display_name: null },
      }),
    );
  });

  it('shows the refusal of an invalid name', async () => {
    setLanguage('en');
    vi.spyOn(api, 'PATCH').mockResolvedValue({
      error: { status: 400, reason: 'invalid', detail: 'a display name has at most 64 characters' },
      response: new Response(null, { status: 400 }),
    } as never);
    await renderPage(AgentsPage, data(['host_groups']));
    fireEvent.click(screen.getByRole('button', { name: 'Display name' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Display name'), {
      target: { value: 'x'.repeat(65) },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await within(dialog).findByRole('alert')).toBeTruthy();
  });
});
