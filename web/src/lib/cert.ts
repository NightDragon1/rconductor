// SPDX-License-Identifier: AGPL-3.0-or-later

// certWarnDays: the panel warns this many days before the trusted
// certificate ends (D-072).
const certWarnDays = 30;

// certNotice is the warning about the end of validity of the trusted
// certificate, or null while it is far away or unknown (D-072).
export function certNotice(notAfter: string | undefined, now: Date): 'soon' | 'expired' | null {
  if (!notAfter) return null;
  const end = new Date(notAfter).getTime();
  if (Number.isNaN(end)) return null;
  if (end <= now.getTime()) return 'expired';
  return end - now.getTime() <= certWarnDays * 24 * 60 * 60 * 1000 ? 'soon' : null;
}
