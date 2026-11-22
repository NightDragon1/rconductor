// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { SmtpPanel } from './SmtpPanel';

const key = ['settings', 'smtp'];
const setup = {
  host: 'mail.example.org',
  port: 587,
  encryption: 'starttls',
  username: 'mailer',
  password_set: true,
  password_state: 'set',
  from: 'panel@example.org',
  version: '3',
};

afterEach(() => vi.restoreAllMocks());

describe('SMTP setup (NFR-017)', () => {
  it('shows the password only as set and hides user and password without TLS', async () => {
    setLanguage('en');
    await renderPage(SmtpPanel, [[key, setup]]);
    const panel = screen.getByRole('region', { name: 'Mail (SMTP)' });
    const pw = within(panel).getByLabelText('Password') as HTMLInputElement;
    expect(pw.value).toBe('');
    expect(pw.placeholder).toBe('set; type to replace');
    fireEvent.change(within(panel).getByLabelText('Encryption'), { target: { value: 'none' } });
    expect(within(panel).queryByLabelText('Password')).toBeNull();
    expect(within(panel).queryByLabelText('User')).toBeNull();
    expect(panel.textContent).toContain(
      'Without encryption the panel sends no user and no password.',
    );
  });

  it('tells a password it cannot read and how to set it again (NFR-017)', async () => {
    setLanguage('en');
    await renderPage(SmtpPanel, [[key, { ...setup, password_state: 'unreadable' }]]);
    const panel = screen.getByRole('region', { name: 'Mail (SMTP)' });
    const pw = within(panel).getByLabelText('Password') as HTMLInputElement;
    expect(pw.placeholder).toBe('set, but not readable');
    expect(panel.textContent).toContain('Enter the password again and save.');
  });

  it('shows no hint for a readable or missing password', async () => {
    setLanguage('en');
    await renderPage(SmtpPanel, [[key, { ...setup, password_set: false, password_state: 'none' }]]);
    const panel = screen.getByRole('region', { name: 'Mail (SMTP)' });
    expect((within(panel).getByLabelText('Password') as HTMLInputElement).placeholder).toBe(
      'not set',
    );
    expect(panel.textContent).not.toContain('cannot be read');
  });

  it('names only the category of a failed test mail', async () => {
    setLanguage('en');
    const post = vi.spyOn(api, 'POST').mockResolvedValue({
      data: { ok: false, category: 'tls' },
      response: new Response(null, { status: 200 }),
    } as never);
    await renderPage(SmtpPanel, [[key, setup]]);
    fireEvent.click(screen.getByRole('button', { name: 'Send a test mail' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Recipient'), {
      target: { value: ' someone@example.com ' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Send' }));
    await waitFor(() =>
      expect(within(dialog).getByRole('status').textContent).toBe(
        'Not sent: no secure connection (STARTTLS missing or certificate not trusted).',
      ),
    );
    expect(post).toHaveBeenCalledWith('/settings/smtp/test', {
      body: { to: 'someone@example.com' },
    });
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(SmtpPanel, [[key, setup]]);
    expect(unmarked(container)).toEqual([]);
  });
});
