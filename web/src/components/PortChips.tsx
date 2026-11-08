// SPDX-License-Identifier: AGPL-3.0-or-later
import { useTranslation } from 'react-i18next';
import type { Port } from '@/lib/ports';

// PortChips shows ports as chips: the host port if published, else the
// container port marked as not published; a Steam link where the profile
// offers it (UI-005, Design 6).
export function PortChips({ ports, label }: { ports: Port[]; label: string }) {
  const { t } = useTranslation();
  if (ports.length === 0) return null;
  return (
    <ul aria-label={label} className="flex flex-wrap gap-1.5">
      {ports.map((p) => (
        <li
          key={`${p.kind}-${p.private}-${p.proto}`}
          className="flex items-center gap-1.5 rounded-md border border-border px-2 py-0.5 font-mono text-xs"
        >
          <span className="text-muted">
            <bdi>{p.name ?? t(`ports.kind.${p.kind}`)}</bdi>
          </span>
          <span>
            {t('ports.value', {
              port: p.public ?? p.private,
              proto: p.proto === 'tcp' || p.proto === 'udp' ? t(`ports.proto.${p.proto}`) : p.proto,
            })}
          </span>
          {p.public === undefined && <span className="text-muted">{t('ports.internal')}</span>}
          {p.steam_url && (
            <a href={p.steam_url} className="tap text-accent underline">
              {t('ports.steam')}
            </a>
          )}
        </li>
      ))}
    </ul>
  );
}
