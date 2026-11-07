// SPDX-License-Identifier: AGPL-3.0-or-later
import { act, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { phoneQuery, usePhone, useReveal } from './phone';

function fakeMedia(matches: boolean) {
  const listeners = new Set<() => void>();
  const m = {
    matches,
    addEventListener: (_: string, l: () => void) => listeners.add(l),
    removeEventListener: (_: string, l: () => void) => listeners.delete(l),
  };
  const matchMedia = vi.fn(() => m);
  vi.stubGlobal('matchMedia', matchMedia);
  return {
    matchMedia,
    listeners,
    set(next: boolean) {
      m.matches = next;
      listeners.forEach((l) => l());
    },
  };
}

function Probe() {
  return <p>{usePhone() ? 'phone' : 'wide'}</p>;
}

function Panel() {
  const ref = useReveal<HTMLDivElement>();
  return <div ref={ref}>panel</div>;
}

afterEach(() => vi.unstubAllGlobals());

describe('usePhone (UI-012)', () => {
  it('follows the width', () => {
    const media = fakeMedia(false);
    render(<Probe />);
    expect(media.matchMedia).toHaveBeenCalledWith(phoneQuery);
    expect(screen.getByText('wide')).toBeTruthy();
    act(() => media.set(true));
    expect(screen.getByText('phone')).toBeTruthy();
  });
  it('stops listening when gone', () => {
    const media = fakeMedia(true);
    const { unmount } = render(<Probe />);
    expect(media.listeners.size).toBe(1);
    unmount();
    expect(media.listeners.size).toBe(0);
  });
  it('is wide without matchMedia', () => {
    vi.stubGlobal('matchMedia', undefined);
    render(<Probe />);
    expect(screen.getByText('wide')).toBeTruthy();
  });
});

describe('useReveal', () => {
  const scroll = vi.fn();
  afterEach(() => {
    scroll.mockClear();
    Reflect.deleteProperty(Element.prototype, 'scrollIntoView');
  });
  it('scrolls to the panel on a phone only', () => {
    Element.prototype.scrollIntoView = scroll;
    fakeMedia(false);
    const wide = render(<Panel />);
    expect(scroll).not.toHaveBeenCalled();
    wide.unmount();
    fakeMedia(true);
    render(<Panel />);
    expect(scroll).toHaveBeenCalledOnce();
  });
});
