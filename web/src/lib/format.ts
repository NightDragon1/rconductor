// SPDX-License-Identifier: AGPL-3.0-or-later
import type { TFunction } from 'i18next';
import { useEffect, useState } from 'react';

// span renders the time between two instants coarsely: seconds, minutes,
// hours or days, always through the language file (NFR-007).
export function span(t: TFunction, ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return t('time.seconds', { count: s });
  if (s < 3600) return t('time.minutes', { count: Math.floor(s / 60) });
  if (s < 86_400) return t('time.hours', { count: Math.floor(s / 3600) });
  return t('time.days', { count: Math.floor(s / 86_400) });
}

export function ago(t: TFunction, iso: string, now: number): string {
  return t('time.ago', { span: span(t, now - Date.parse(iso)) });
}

export function clock(lng: string, iso: string): string {
  return new Intl.DateTimeFormat(lng, { hour: '2-digit', minute: '2-digit' }).format(new Date(iso));
}

export function dateTime(lng: string, iso: string): string {
  return new Intl.DateTimeFormat(lng, { dateStyle: 'short', timeStyle: 'short' }).format(
    new Date(iso),
  );
}

export function decimal(lng: string, v: number, digits = 1): string {
  return new Intl.NumberFormat(lng, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(v);
}

export const gib = (bytes: number) => bytes / 2 ** 30;

// useNow ticks so that ages and stale marks stay current.
export function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
