// SPDX-License-Identifier: AGPL-3.0-or-later
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyTheme, startTheme, storedTheme } from './theme';

const design = () => document.documentElement.dataset.theme;

describe('design (UI-001)', () => {
  let listener: () => void = () => undefined;
  const m = {
    matches: false,
    addEventListener: (_: string, f: () => void) => {
      listener = f;
    },
  };
  beforeEach(() => {
    localStorage.clear();
    m.matches = false;
    vi.stubGlobal('matchMedia', () => m);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('follows the system by default, also when it changes', () => {
    expect(storedTheme()).toBe('auto');
    startTheme();
    expect(design()).toBe('dark');
    m.matches = true;
    listener();
    expect(design()).toBe('light');
  });

  it('keeps a choice against the system', () => {
    applyTheme('dark');
    m.matches = true;
    startTheme();
    listener();
    expect(storedTheme()).toBe('dark');
    expect(design()).toBe('dark');
  });

  it('does not store anything without a choice', () => {
    startTheme();
    expect(localStorage.length).toBe(0);
  });
});
