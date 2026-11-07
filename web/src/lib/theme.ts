// SPDX-License-Identifier: AGPL-3.0-or-later

// The design follows the system unless light or dark is chosen (Design
// 6.1, UI-001); the choice is a per-browser convenience in localStorage.
export type Theme = 'auto' | 'light' | 'dark';
export const themes: Theme[] = ['auto', 'light', 'dark'];
const key = 'rcd.design';
const query = '(prefers-color-scheme: light)';

export function storedTheme(): Theme {
  try {
    const v = localStorage.getItem(key);
    return v === 'light' || v === 'dark' ? v : 'auto';
  } catch {
    return 'auto';
  }
}

const media = () => (typeof window.matchMedia === 'function' ? window.matchMedia(query) : null);

function show(t: Theme) {
  const light = t === 'light' || (t === 'auto' && media()?.matches === true);
  document.documentElement.dataset.theme = light ? 'light' : 'dark';
}

// startTheme shows the stored design and follows the system while it is
// on auto.
export function startTheme() {
  show(storedTheme());
  media()?.addEventListener('change', () => show(storedTheme()));
}

export function applyTheme(t: Theme) {
  show(t);
  try {
    localStorage.setItem(key, t);
  } catch {
    // private mode: the choice lasts for this page only
  }
}
