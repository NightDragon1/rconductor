// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { SettingsPage, settingsKey } from './Settings';

const data: [readonly unknown[], unknown][] = [
  [settingsKey, { values: { 'ui.shortcuts_enabled': false }, etag: '"1"' }],
];
const help = 'Single keys for frequent actions, for everyone.';

describe('explanations in the settings (UI-014)', () => {
  beforeEach(async () => {
    localStorage.clear();
    await setLanguage('en');
  });

  it('hides the explanations and keeps that after a reload', async () => {
    await renderPage(SettingsPage, data);
    expect(screen.queryByText(help)).not.toBeNull();
    const toggle = screen.getByRole('checkbox', { name: 'Show explanations' });
    expect((toggle as HTMLInputElement).checked).toBe(true);

    fireEvent.click(toggle);
    expect(screen.queryByText(help)).toBeNull();

    cleanup();
    await renderPage(SettingsPage, data);
    expect(
      (screen.getByRole('checkbox', { name: 'Show explanations' }) as HTMLInputElement).checked,
    ).toBe(false);
    expect(screen.queryByText(help)).toBeNull();
    expect(screen.queryByText('Keyboard shortcuts')).not.toBeNull();

    fireEvent.click(screen.getByRole('checkbox', { name: 'Show explanations' }));
    expect(screen.queryByText(help)).not.toBeNull();
  });

  it('shows the explanations when the browser storage is unavailable', async () => {
    const original = Storage.prototype.getItem;
    Storage.prototype.getItem = () => {
      throw new Error('blocked');
    };
    try {
      await renderPage(SettingsPage, data);
      expect(screen.queryByText(help)).not.toBeNull();
    } finally {
      Storage.prototype.getItem = original;
    }
  });
});

// Every global setting (internal/users/settings.go), the rate limits of
// each role.
const allKeys: Record<string, unknown> = {
  'backup.interval': '24h0m0s',
  'backup.keep_auto': 7,
  'backup.keep_pre_migration': 3,
  'session.idle_timeout': '12h0m0s',
  'session.max_lifetime': '168h0m0s',
  'login.max_failures': 5,
  'login.max_client_failures': 50,
  'login.max_tracked': 10000,
  'login.lockout_duration': '15m0s',
  'login.overflow_gap': '200ms',
  'login.overflow_line': 50,
  'invite.lifetime': '72h0m0s',
  'supporters.enabled': true,
  'audit.retention_days': 90,
  'ws.max_sockets_per_session': 8,
  'ws.max_subscriptions_per_socket': 16,
  'profiles.poll_interval': '1m0s',
  'agents.fingerprint_pairing': false,
  'ui.shortcuts_enabled': false,
  'servers.connection_test_per_minute': 10,
  'servers.http_parallel': 4,
  'agent.version_retry': '30s',
  'agent.version_window': '10m0s',
  'agent.volume_interval': '1h0m0s',
  'agent.volume_stale': '6h0m0s',
  'auth.totp_required': false,
  'auth.second_factor_window': '5m0s',
  'auth.recovery_lifetime': '1h0m0s',
  'auth.recovery_per_hour': 3,
  'auth.recovery_workers': 2,
  'auth.recovery_queue': 100,
  'smtp.timeout': '30s',
  'smtp.test_per_minute': 2,
  'messages.max_text': 2000,
  'messages.max_recipients': 50,
  'messages.max_resolved_recipients': 1000,
  'messages.per_hour': 20,
  'messages.max_comment': 500,
  'messages.comments_per_hour': 60,
  'messages.retention_days': 365,
  'agents.display_name_max': 64,
  'system_log.max_bytes': 1048576,
  'diagnostics.max_bytes': 1048576,
  'diagnostics.parallel': 1,
  'commands.templates_per_server': 50,
  'commands.multi_max_servers': 20,
  'commands.multi_parallel': 4,
  ...Object.fromEntries(
    ['rcon_admin', 'server_admin', 'moderator', 'supporter'].flatMap((r) => [
      [`ratelimit.${r}.per_minute`, 60],
      [`ratelimit.${r}.burst`, 10],
    ]),
  ),
  'some.new_key': 1,
};
const groups = [
  'Security',
  'Users',
  'Messages',
  'Servers and commands',
  'Agents',
  'System',
  'Other',
];
const advanced = [
  'login.max_tracked',
  'login.overflow_gap',
  'login.overflow_line',
  'auth.recovery_workers',
  'auth.recovery_queue',
  'commands.multi_parallel',
  'servers.http_parallel',
  'profiles.poll_interval',
  'ws.max_subscriptions_per_socket',
  'agent.version_retry',
  'agent.version_window',
  'agent.volume_interval',
  'agent.volume_stale',
  'diagnostics.parallel',
];

describe('settings in groups from 1024 px (O8)', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal('matchMedia', (q: string) => ({
      matches: q.includes('min-width'),
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });
  const render = () => renderPage(SettingsPage, [[settingsKey, { values: allKeys, etag: '"1"' }]]);
  const nav = () => screen.getByRole('navigation', { name: 'Settings groups' });
  const open = (name: string) => fireEvent.click(within(nav()).getByRole('button', { name }));
  const shownKeys = () => [...document.querySelectorAll('form code')].map((c) => c.textContent);

  it('shows every setting in exactly one group, tuning values behind Advanced', async () => {
    await setLanguage('en');
    await render();
    expect(
      within(nav())
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual(groups);
    const seen: string[] = [];
    for (const g of groups) {
      open(g);
      expect(screen.getByRole('heading', { level: 2, name: g })).not.toBeNull();
      expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1);
      for (const d of document.querySelectorAll('form details')) {
        expect((d as HTMLDetailsElement).open).toBe(false);
        expect(d.querySelector('summary')?.textContent).toBe('Advanced');
      }
      const inside = [...document.querySelectorAll('form details code')].map((c) => c.textContent);
      expect(inside.every((k) => advanced.includes(k ?? ''))).toBe(true);
      seen.push(...shownKeys().map(String));
    }
    expect(seen.sort()).toEqual(Object.keys(allKeys).sort());
    open('Security');
    expect(screen.getByRole('region', { name: 'Sign-in protection' })).not.toBeNull();
    expect(screen.getByRole('region', { name: 'Two-factor sign-in' })).not.toBeNull();
    expect(screen.getByRole('region', { name: 'Sessions' })).not.toBeNull();
    open('System');
    expect(screen.getByRole('region', { name: 'Diagnostics' }).textContent).toContain(
      'system_log.max_bytes',
    );
  });

  it('keeps changes over a change of group and saves them as before (D-048)', async () => {
    await setLanguage('en');
    const patch = vi.spyOn(api, 'PATCH').mockResolvedValue({
      data: {},
      response: new Response(null, { status: 200, headers: { ETag: '"2"' } }),
    } as never);
    await render();
    open('Users');
    fireEvent.change(screen.getByLabelText(/invite\.lifetime/), { target: { value: '48 h' } });
    open('System');
    fireEvent.click(screen.getByRole('checkbox', { name: /ui\.shortcuts_enabled/ }));
    open('Security');
    fireEvent.click(screen.getByRole('button', { name: 'Save 2 changes' }));
    await waitFor(() => expect(patch).toHaveBeenCalledOnce());
    expect(patch.mock.calls[0]?.[1]).toMatchObject({
      params: { header: { 'If-Match': '"1"' } },
      body: { 'invite.lifetime': '48h', 'ui.shortcuts_enabled': true },
    });
  });

  it('keeps the chosen group over a reload', async () => {
    await setLanguage('en');
    await render();
    open('Agents');
    cleanup();
    await render();
    expect(within(nav()).getByRole('button', { name: 'Agents' }).getAttribute('aria-current')).toBe(
      'true',
    );
    expect(shownKeys()).toContain('agents.display_name_max');
  });

  it('has no text outside the language files in any group (NFR-007)', async () => {
    await setLanguage('xx');
    const { container } = await render();
    for (const b of within(screen.getByRole('navigation')).getAllByRole('button')) {
      fireEvent.click(b);
      for (const d of container.querySelectorAll('form details'))
        (d as HTMLDetailsElement).open = true;
      // Keys are data; a key without texts shows by its name.
      const data = new Set(Object.keys(allKeys));
      expect(unmarked(container).filter((s) => !data.has(s))).toEqual([]);
    }
  });
});
