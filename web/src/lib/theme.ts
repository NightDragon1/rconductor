// SPDX-License-Identifier: AGPL-3.0-or-later

// Dark is the default design, light can be chosen (Design 6.1); the choice
// is a per-browser convenience in localStorage.
export type Theme = 'dark' | 'light';
const key = 'rcd.theme';

export function storedTheme(): Theme {
  try {
    return localStorage.getItem(key) === 'light' ? 'light' : 'dark';
  } catch {
    return 'dark';
  }
}

export function applyTheme(t: Theme) {
  document.documentElement.dataset.theme = t;
  try {
    localStorage.setItem(key, t);
  } catch {
    // private mode: the choice lasts for this page only
  }
}
