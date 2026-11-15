// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { Info, TriangleAlert } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { licenseUrl, thirdPartyUrl, useAbout, webUrl, type About } from '@/api/about';
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
  const { data, isError } = useAbout(session?.user.id, open);
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

// AboutContent shows one answer of GET /about: build and agents only when
// the panel sent them.
export function AboutContent({ about }: { about: About }) {
  const { t } = useTranslation();
  const repo = webUrl(about.repo_url);
  const built = about.build_date && !Number.isNaN(Date.parse(about.build_date));
  return (
    <div className="flex flex-col gap-4 text-sm">
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

      {about.agents !== undefined && <AgentVersions about={about} />}
    </div>
  );
}

function AgentVersions({ about }: { about: About }) {
  const { t } = useTranslation();
  const agents = about.agents ?? [];
  const panelMajor = majorVersion(about.version);
  return (
    <section aria-labelledby="about-agents" className="space-y-2">
      <h2 id="about-agents" className="font-semibold text-text">
        {t('about.agents')}
      </h2>
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
