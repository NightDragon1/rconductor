// SPDX-License-Identifier: AGPL-3.0-or-later
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { initI18n } from '@/i18n';
import { PasswordInput, SHOW_MS } from './PasswordInput';

const field = () => screen.getByLabelText('Secret') as HTMLInputElement;
const show = () => fireEvent.click(screen.getByRole('button', { name: 'Show password' }));

describe('PasswordInput (UI-013)', () => {
  beforeEach(() => {
    initI18n();
    vi.useFakeTimers();
    render(
      <form onSubmit={(e) => e.preventDefault()}>
        <PasswordInput aria-label="Secret" />
      </form>,
    );
  });
  afterEach(() => vi.useRealTimers());

  it('starts hidden, shows on demand and hides by hand', () => {
    expect(field().type).toBe('password');
    show();
    expect(field().type).toBe('text');
    fireEvent.click(screen.getByRole('button', { name: 'Hide password' }));
    expect(field().type).toBe('password');
  });

  it('hides again after the time', () => {
    show();
    act(() => {
      vi.advanceTimersByTime(SHOW_MS - 1);
    });
    expect(field().type).toBe('text');
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(field().type).toBe('password');
  });

  it('cannot be shown when the field is locked (UI-012)', () => {
    render(<PasswordInput aria-label="Locked" disabled defaultValue="" />);
    const toggles = screen.getAllByRole('button', { name: 'Show password' });
    expect((toggles.at(-1) as HTMLButtonElement).disabled).toBe(true);
  });

  it('hides before the form goes out', () => {
    show();
    fireEvent.submit(field().form as HTMLFormElement);
    expect(field().type).toBe('password');
  });
});
