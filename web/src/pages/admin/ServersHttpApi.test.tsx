// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { agentsKey, profilesKey, serverGroupsKey, serversKey } from '@/api/servers';
import { sessionKey } from '@/api/session';
import { renderPage, setLanguage } from '@/test/render';
import { certNotice } from '@/lib/cert';
import { ServersAdminPage } from './ServersAdmin';

const now = new Date().toISOString();
const day = 24 * 60 * 60 * 1000;
const pinned = 'ab'.repeat(32);
const shown = 'cd'.repeat(32);

const data = (notAfter?: string): [readonly unknown[], unknown][] => [
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
        name: 'factory',
        groups: [],
        status: 'cert_mismatch',
        status_since: now,
        version: 'v',
        profile_id: 'sf',
        host: 'sf.lan',
        port: 7777,
        credentials: 'ok',
        tls_fingerprint: pinned,
        tls_not_after: notAfter,
      },
    ],
  ],
  [serverGroupsKey, []],
  [profilesKey, [{ id: 'sf', name: 'Satisfactory', protocol: 'http-api' }]],
  [agentsKey, []],
  [['settings'], { values: { 'agents.fingerprint_pairing': false }, etag: '"e"' }],
];

afterEach(() => vi.restoreAllMocks());

describe('certNotice (D-072)', () => {
  const at = new Date('2026-11-09T12:00:00Z');
  it('warns 30 days before and after the end', () => {
    expect(certNotice(undefined, at)).toBeNull();
    expect(certNotice('not a date', at)).toBeNull();
    expect(certNotice(new Date(at.getTime() + 31 * day).toISOString(), at)).toBeNull();
    expect(certNotice(new Date(at.getTime() + 30 * day).toISOString(), at)).toBe('soon');
    expect(certNotice(new Date(at.getTime() + 1000).toISOString(), at)).toBe('soon');
    expect(certNotice(at.toISOString(), at)).toBe('expired');
    expect(certNotice(new Date(at.getTime() - day).toISOString(), at)).toBe('expired');
  });
});

describe('server with an HTTPS API (PROT-003)', () => {
  it('asks for an API token and shows the trusted certificate with its warning', async () => {
    setLanguage('en');
    await renderPage(ServersAdminPage, data(new Date(Date.now() + 10 * day).toISOString()));
    fireEvent.click(screen.getByRole('button', { name: 'factory' }));
    expect(screen.getByLabelText('New API token')).toBeTruthy();
    expect(screen.getByLabelText('API port')).toBeTruthy();
    expect(screen.getByTestId('trusted-fingerprint').textContent).toBe(
      pinned.replace(/(..)(?!$)/g, '$1:'),
    );
    expect(screen.getByRole('status').textContent).toContain('The trusted certificate ends on');
  });

  it('shows no warning while the end is far away', async () => {
    setLanguage('en');
    await renderPage(ServersAdminPage, data(new Date(Date.now() + 90 * day).toISOString()));
    fireEvent.click(screen.getByRole('button', { name: 'factory' }));
    expect(screen.queryByText(/The trusted certificate ends on/)).toBeNull();
    expect(screen.getByText(/^Valid until/)).toBeTruthy();
  });

  it('trusts the certificate the test read and sends it with save', async () => {
    setLanguage('en');
    const post = vi.spyOn(api, 'POST').mockResolvedValue({
      data: { result: 'cert_mismatch', fingerprint: shown, not_after: '2027-11-09T00:00:00Z' },
      response: new Response(),
    } as never);
    const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({
      data: { id: 's1', name: 'factory', version: 'w', tls_fingerprint: shown },
      response: new Response(),
    } as never);
    await renderPage(ServersAdminPage, data());
    fireEvent.click(screen.getByRole('button', { name: 'factory' }));
    expect(screen.getByText('End of validity not known yet')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(screen.getByTestId('seen-fingerprint')).toBeTruthy());
    expect(post).toHaveBeenCalledWith(
      '/servers/test-connection',
      expect.objectContaining({ body: expect.objectContaining({ tls_fingerprint: pinned }) }),
    );
    expect(
      screen.getByText(
        'The game server shows another certificate than the trusted one. Nothing was sent.',
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Trust this certificate' }));
    expect(screen.getByText('Trusted when you save.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(patch).toHaveBeenCalled());
    expect(patch.mock.calls[0]?.[1]).toMatchObject({ body: { tls_fingerprint: shown } });
  });
});
