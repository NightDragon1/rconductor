// SPDX-License-Identifier: AGPL-3.0-or-later

// Durations of the settings come as Go durations ("72h0m0s", "15m0s",
// "30s"); the page shows and takes them as "72 h", "15 min", "30 s".

const goPart = /(\d+(?:\.\d+)?)(h|m|s|ms)/g;
const goDuration = /^(\d+(?:\.\d+)?(?:h|m|s|ms))+$/;

// isGoDuration tells a duration value from other strings.
export const isGoDuration = (v: unknown): v is string =>
  typeof v === 'string' && goDuration.test(v);

function seconds(go: string): number {
  let total = 0;
  for (const [, n, unit] of go.matchAll(goPart)) {
    const v = Number(n);
    total += unit === 'h' ? v * 3600 : unit === 'm' ? v * 60 : unit === 's' ? v : v / 1000;
  }
  return total;
}

// formatDuration shows a Go duration in the largest unit that fits
// exactly: hours, minutes or seconds.
export function formatDuration(go: string): string {
  const s = seconds(go);
  if (s > 0 && s % 3600 === 0) return `${s / 3600} h`;
  if (s > 0 && s % 60 === 0) return `${s / 60} min`;
  return `${s} s`;
}

const units: Record<string, string> = {
  h: 'h',
  hr: 'h',
  hrs: 'h',
  hour: 'h',
  hours: 'h',
  m: 'm',
  min: 'm',
  mins: 'm',
  minute: 'm',
  minutes: 'm',
  s: 's',
  sec: 's',
  secs: 's',
  second: 's',
  seconds: 's',
};

// parseDuration reads "72 h", "15 min", "30 s" (also "72h", "1.5 h") into
// a Go duration; null when it is none. The backend checks the range.
export function parseDuration(text: string): string | null {
  const m = /^\s*(\d+(?:[.,]\d+)?)\s*([a-zA-Z]+)\s*$/.exec(text);
  if (!m) return null;
  const unit = units[m[2]?.toLowerCase() ?? ''];
  const n = Number((m[1] ?? '').replace(',', '.'));
  if (!unit || !Number.isFinite(n) || n <= 0) return null;
  if (Number.isInteger(n)) return `${n}${unit}`;
  // Go takes fractions only through smaller units.
  const s = Math.round(n * (unit === 'h' ? 3600 : unit === 'm' ? 60 : 1));
  return `${s}s`;
}
