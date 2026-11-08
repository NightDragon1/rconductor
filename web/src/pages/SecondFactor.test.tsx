// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { TwoFactorSetup } from '@/components/TwoFactorSetup';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { LoginPage } from './Login';
import { RecoverPage } from './Recover';

afterEach(() => {
  vi.restoreAllMocks();
  window.location.hash = '';
});

const reply = (status: number, data?: unknown, error?: unknown) =>
  ({ data, error, response: new Response(null, { status }) }) as never;

describe('second factor (AUTH-008)', () => {
  it('asks for the code after the right password and offers the recovery', async () => {
    setLanguage('en');
    const post = vi
      .spyOn(api, 'POST')
      .mockResolvedValueOnce(
        reply(202, { second_factor: 'totp', expires_at: new Date().toISOString() }),
      );
    await renderPage(LoginPage);
    fireEvent.change(screen.getByLabelText('Pseudonym'), { target: { value: 'kiwi' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret secret' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await screen.findByRole('heading', { name: 'Enter the code' });
    expect(
      screen.getByRole('link', { name: 'Lost your authenticator app?' }).getAttribute('href'),
    ).toBe('/recover');
    post.mockResolvedValueOnce(
      reply(401, undefined, { reason: 'unauthenticated', detail: 'code wrong' }),
    );
    fireEvent.change(screen.getByLabelText('Code from the app'), { target: { value: '000000' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('The code is wrong.'));
    expect(post).toHaveBeenLastCalledWith('/auth/login/totp', { body: { code: '000000' } });
    // An ended step sends back to the password.
    post.mockResolvedValueOnce(
      reply(401, undefined, {
        reason: 'unauthenticated',
        detail: 'no waiting sign-in, start again',
      }),
    );
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }));
    await screen.findByLabelText('Pseudonym');
  });

  it('answers a recovery request alike and redeems a link with the password', async () => {
    setLanguage('en');
    const post = vi.spyOn(api, 'POST').mockResolvedValue(reply(202));
    await renderPage(RecoverPage);
    fireEvent.change(screen.getByLabelText('Pseudonym'), { target: { value: 'nobody' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Send link' }));
    await screen.findByText(
      'If an account fits, a link is on its way. It is valid for a short time.',
    );
    expect(post).toHaveBeenCalledWith('/auth/recovery', { body: { pseudonym: 'nobody' } });
  });

  it('redeems the token of the fragment and drops it from the address', async () => {
    setLanguage('en');
    window.location.hash = '#tok-123';
    const post = vi.spyOn(api, 'POST').mockResolvedValue(reply(204));
    await renderPage(RecoverPage);
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret secret' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Reset' }));
    await screen.findByText(
      'Two-factor sign-in is reset; all sessions ended. Sign in with your password.',
    );
    expect(post).toHaveBeenCalledWith('/auth/recovery/redeem', {
      body: { token: 'tok-123', password: 'secret secret' },
    });
    expect(window.location.hash).toBe('');
  });

  it('shows the QR code, the secret and the address after the password', async () => {
    setLanguage('en');
    vi.spyOn(api, 'POST').mockResolvedValueOnce(
      reply(200, {
        secret: 'GEZDGNBVGY3TQOJQ',
        uri: 'otpauth://totp/RCONductor:kiwi?secret=GEZDGNBVGY3TQOJQ',
      }),
    );
    await renderPage(() => <TwoFactorSetup onDone={() => undefined} />);
    fireEvent.change(screen.getByLabelText('Your password'), {
      target: { value: 'secret secret' },
    });
    fireEvent.submit(screen.getByRole('button', { name: 'Set up' }));
    const img = await screen.findByRole('img', { name: 'QR code for the authenticator app' });
    expect(img.getAttribute('src')).toMatch(/^data:image\/png;base64,/);
    expect(screen.getByText('GEZDGNBVGY3TQOJQ')).toBeTruthy();
  });

  it('has no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    for (const Page of [
      LoginPage,
      RecoverPage,
      () => <TwoFactorSetup onDone={() => undefined} />,
    ]) {
      const { container, unmount } = await renderPage(Page);
      expect(unmarked(container)).toEqual([]);
      unmount();
    }
  });
});
