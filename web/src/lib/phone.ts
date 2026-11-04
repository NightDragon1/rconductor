// SPDX-License-Identifier: AGPL-3.0-or-later
import { useEffect, useRef, useSyncExternalStore } from 'react';

// Below Tailwind's md: the width where the sidebar becomes a menu.
export const phoneQuery = '(max-width: 47.99rem)';

const media = () =>
  typeof window.matchMedia === 'function' ? window.matchMedia(phoneQuery) : null;

function subscribe(onChange: () => void) {
  const m = media();
  m?.addEventListener('change', onChange);
  return () => m?.removeEventListener('change', onChange);
}

// usePhone tells whether the page shows the phone view (UI-012). It only
// decides what is shown: the backend checks every request the same.
export function usePhone(): boolean {
  return useSyncExternalStore(subscribe, () => media()?.matches ?? false);
}

// useReveal scrolls a panel into view on a phone, where it sits below the
// list instead of beside it.
export function useReveal<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const phone = usePhone();
  useEffect(() => {
    if (phone) ref.current?.scrollIntoView({ block: 'start' });
  }, [phone]);
  return ref;
}
