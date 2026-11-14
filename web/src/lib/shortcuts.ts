// SPDX-License-Identifier: AGPL-3.0-or-later
import { useEffect, useRef } from 'react';
import { useSession } from '@/api/session';

// useShortcuts binds single keys to actions (UI-008): only when the
// RCON admin has switched them on globally (ui.shortcuts_enabled, off by
// default) and never while typing in a field.
export function useShortcuts(keys: Record<string, () => void>) {
  const { data: session } = useSession();
  const on = !!session?.ui.shortcuts_enabled;
  const ref = useRef(keys);
  useEffect(() => {
    ref.current = keys;
  });
  useEffect(() => {
    if (!on) return;
    const handler = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      const typing =
        el?.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el?.tagName ?? '');
      if (e.ctrlKey || e.metaKey || e.altKey || (typing && e.key !== 'Escape')) return;
      const f = ref.current[e.key];
      if (f) {
        e.preventDefault();
        f();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [on]);
}

// usePaletteKey opens the command palette on Ctrl+K (Cmd+K on a Mac),
// also from a field (CMD-016): only while the palette is there (enabled)
// and the RCON admin has switched keyboard shortcuts on.
export function usePaletteKey(enabled: boolean, open: () => void) {
  const { data: session } = useSession();
  const on = enabled && !!session?.ui.shortcuts_enabled;
  const ref = useRef(open);
  useEffect(() => {
    ref.current = open;
  });
  useEffect(() => {
    if (!on) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== 'k' || !(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey)
        return;
      e.preventDefault();
      ref.current();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [on]);
}
