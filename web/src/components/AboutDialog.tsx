// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { Info, TriangleAlert } from 'lucide-react';
import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import {
  licenseUrl,
  thirdPartyUrl,
  useAbout,
  useThirdParty,
  webUrl,
  type About,
  type ThirdParty,
} from '@/api/about';
import { useSession } from '@/api/session';
import { ForeignText } from '@/components/ForeignText';
import { Logo } from '@/components/Logo';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { majorDiffers, majorVersion } from '@/lib/version';

function External({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="tap text-text underline underline-offset-2"
    >
      {children}
    </a>
  );
}

// AboutButton is the entry at the foot of the navigation (UI-024); the
// panel answers with what the role may see, the dialog shows that.
export function AboutButton({ collapsed }: { collapsed: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <div className="flex justify-center px-3 pb-3">
      <Button
        variant="ghost"
        size={collapsed ? 'icon' : 'default'}
        onClick={() => setOpen(true)}
        title={collapsed ? t('about.open') : undefined}
      >
        <Info aria-hidden />
        {collapsed ? <span className="sr-only">{t('about.open')}</span> : t('about.open')}
      </Button>
      <AboutDialog open={open} onClose={() => setOpen(false)} />
    </div>
  );
}

function AboutDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const { data: session } = useSession();
  // Only with a known user: the entry of an anonymous caller may hold an
  // answer fetched while a session was still sent along (D-080).
  const user = session?.user.id;
  const about = useAbout(user, open && user !== undefined);
  // The empty entry is never shown, even when it holds an answer.
  const data = user === undefined ? undefined : about.data;
  const isError = user !== undefined && about.isError;
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
        <DialogTitle className="sr-only">{t('about.title')}</DialogTitle>
        {data ? (
          <AboutContent about={data} />
        ) : (
          <DialogDescription role={isError ? 'alert' : undefined}>
            {t(isError ? 'app.error' : 'app.loading')}
          </DialogDescription>
        )}
        <div className="flex justify-end">
          <DialogClose asChild>
            <Button variant="ghost">{t('about.close')}</Button>
          </DialogClose>
        </div>
      </DialogContent>
    </Dialog>
  );
}

type Tab = 'about' | 'thirdParty' | 'agents';

// AboutContent shows one answer of GET /about in tabs: the program, the
// third-party components of the build, and the agents only when the panel
// sent them (the RCON admin).
export function AboutContent({ about }: { about: About }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<Tab>('about');
  const third = useThirdParty(true);
  const id = useId();
  const refs = useRef<Partial<Record<Tab, HTMLButtonElement | null>>>({});
  const tabs: { key: Tab; label: string }[] = [
    { key: 'about', label: t('about.tabAbout') },
    {
      key: 'thirdParty',
      label: third.data
        ? t('about.tabThirdPartyCount', { count: third.data.length })
        : t('about.tabThirdParty'),
    },
    ...(about.agents !== undefined ? [{ key: 'agents' as Tab, label: t('about.agents') }] : []),
  ];
  // Arrow keys move between the tabs (WAI-ARIA tabs pattern).
  const onKey = (e: KeyboardEvent) => {
    const i = tabs.findIndex((x) => x.key === tab);
    const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
    if (step === 0) return;
    e.preventDefault();
    const next = tabs[(i + step + tabs.length) % tabs.length]?.key ?? 'about';
    setTab(next);
    refs.current[next]?.focus();
  };
  return (
    <div className="flex flex-col gap-4 text-sm">
      <div
        role="tablist"
        aria-label={t('about.title')}
        className="flex flex-wrap gap-1 border-b border-border"
        onKeyDown={onKey}
      >
        {tabs.map((x) => (
          <button
            key={x.key}
            ref={(el) => {
              refs.current[x.key] = el;
            }}
            type="button"
            role="tab"
            id={`${id}-${x.key}-tab`}
            aria-controls={`${id}-${x.key}`}
            aria-selected={tab === x.key}
            tabIndex={tab === x.key ? 0 : -1}
            onClick={() => setTab(x.key)}
            className={cn(
              '-mb-px min-h-11 border-b-2 px-3 text-sm',
              tab === x.key
                ? 'border-accent font-semibold text-text'
                : 'border-transparent text-muted',
            )}
          >
            {x.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`${id}-${tab}`} aria-labelledby={`${id}-${tab}-tab`} tabIndex={0}>
        {tab === 'about' && <Program about={about} />}
        {tab === 'thirdParty' && <ThirdPartyList list={third.data} failed={third.isError} />}
        {tab === 'agents' && <AgentVersions about={about} />}
      </div>
    </div>
  );
}

// Program is the tab About: name, version, build, author and license.
function Program({ about }: { about: About }) {
  const { t } = useTranslation();
  const repo = webUrl(about.repo_url);
  const built = about.build_date && !Number.isNaN(Date.parse(about.build_date));
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col items-center gap-2 text-center">
        <Logo />
        <p className="text-base font-semibold text-text">
          <bdi>{about.name}</bdi>
        </p>
        <dl className="grid grid-cols-[auto_auto] justify-center gap-x-3 gap-y-1 text-left">
          <dt className="text-muted">{t('about.version')}</dt>
          <dd>
            <bdi>{about.version}</bdi>
          </dd>
          {about.commit !== undefined && (
            <>
              <dt className="text-muted">{t('about.commit')}</dt>
              <dd>
                <code className="wrap-anywhere">{about.commit}</code>
              </dd>
            </>
          )}
          {about.build_date !== undefined && (
            <>
              <dt className="text-muted">{t('about.buildDate')}</dt>
              <dd>
                {built ? (
                  <time dateTime={about.build_date}>{about.build_date}</time>
                ) : (
                  <bdi>{about.build_date}</bdi>
                )}
              </dd>
            </>
          )}
        </dl>
      </div>

      <div className="text-center leading-tight">
        <p className="text-lg font-bold text-text">{t('about.author')}</p>
        <p className="text-xs text-muted">{t('app.org')}</p>
      </div>

      <div className="space-y-2 text-muted">
        <p>{t('about.license')}</p>
        <p>
          {t('about.more')}{' '}
          {repo ? (
            <External href={repo}>
              <bdi className="wrap-anywhere">{repo}</bdi>
            </External>
          ) : (
            <bdi className="wrap-anywhere">{about.repo_url}</bdi>
          )}
        </p>
        <p className="flex flex-wrap gap-x-4">
          <External href={licenseUrl}>{t('about.licenseText')}</External>
          <External href={thirdPartyUrl}>{t('about.thirdParty')}</External>
        </p>
      </div>
    </div>
  );
}

// ThirdPartyList shows the components of the build with license and
// copyright; without a copyright line the license text opens below.
function ThirdPartyList({ list, failed }: { list?: ThirdParty[]; failed: boolean }) {
  const { t } = useTranslation();
  if (!list) return <p className="text-muted">{t(failed ? 'app.error' : 'app.loading')}</p>;
  if (list.length === 0) return <p className="text-muted">{t('about.noThirdParty')}</p>;
  return (
    <ul className="space-y-2" aria-label={t('about.thirdParty')}>
      {list.map((c, i) => (
        <li key={`${c.name}@${c.version}#${i}`} className="rounded-md border border-border p-2">
          <p className="flex flex-wrap items-baseline justify-between gap-x-3">
            <bdi className="font-medium wrap-anywhere text-text">
              {c.name} {c.version}
            </bdi>
            <bdi className="text-xs text-muted">{c.license}</bdi>
          </p>
          {c.copyright.length > 0 ? (
            c.copyright.map((l) => (
              <p key={l} className="text-muted wrap-anywhere">
                <bdi>{l}</bdi>
              </p>
            ))
          ) : (
            <details className="text-muted">
              <summary className="tap cursor-pointer">{t('about.seeLicense')}</summary>
              <pre className="mt-1 max-h-64 overflow-auto text-xs whitespace-pre-wrap wrap-anywhere">
                {c.text}
              </pre>
            </details>
          )}
        </li>
      ))}
    </ul>
  );
}

function AgentVersions({ about }: { about: About }) {
  const { t } = useTranslation();
  const agents = about.agents ?? [];
  const panelMajor = majorVersion(about.version);
  return (
    <section aria-label={t('about.agents')} className="space-y-2">
      {agents.length === 0 ? (
        <p className="text-muted">{t('about.noAgents')}</p>
      ) : (
        <ul className="space-y-2">
          {agents.map((a) => {
            const differs = majorDiffers(a.version, about.version);
            return (
              <li key={a.id} className="rounded-md border border-border p-2">
                <div className="flex flex-wrap items-baseline justify-between gap-x-3">
                  <span className="font-medium wrap-anywhere text-text">
                    <ForeignText text={a.name} />
                  </span>
                  <span className={cn('text-xs', a.connected ? 'text-ok' : 'text-muted')}>
                    {t(a.connected ? 'about.connected' : 'about.disconnected')}
                  </span>
                </div>
                <p className="text-muted">
                  {t('about.agentVersion')}{' '}
                  <span className="wrap-anywhere text-text">
                    <ForeignText text={a.version} />
                  </span>
                </p>
                {differs && (
                  <p role="status" className="mt-1 flex items-start gap-1 text-warn">
                    <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
                    {t('about.majorDiffers', { major: panelMajor })}
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
