// SPDX-License-Identifier: AGPL-3.0-or-later
import { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { useHistory, type HistoryEntry } from '@/api/servers';
import { useSession } from '@/api/session';
import { Button } from '@/components/ui/button';
import { CopyButton } from '@/components/CopyButton';
import { ForeignText } from '@/components/ForeignText';
import { dateTime } from '@/lib/format';
import { isAdmin, stripCodes } from '@/lib/serverpage';
import { cn } from '@/lib/utils';
import { useServer } from '@/api/live-servers';

const resultTone: Record<HistoryEntry['result'], string> = {
  ok: 'text-ok',
  denied: 'text-danger',
  error: 'text-danger',
  timeout: 'text-warn',
};

function Entry({ e }: { e: HistoryEntry }) {
  const { t, i18n } = useTranslation();
  const pre = useRef<HTMLPreElement>(null);
  // Answers are foreign data: text only, color codes dropped (SEC-006).
  const answer = e.response ? stripCodes(e.response) : '';
  return (
    <li className="flex flex-col gap-1.5 border-t border-border px-4 py-3 first:border-t-0">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm">
        <time dateTime={e.ts} className="font-mono text-muted">
          {dateTime(i18n.language, e.ts)}
        </time>
        {e.pseudonym && <bdi className="font-semibold">{e.pseudonym}</bdi>}
        {e.role && <span className="text-muted">{t(`role.${e.role}`)}</span>}
        <code className="rounded bg-surface-raised px-1.5 py-0.5 font-mono">
          <ForeignText text={e.command} />
        </code>
        <span className={cn('ml-auto', resultTone[e.result])}>
          {t(`history.result.${e.result}`)}
          {e.reason && <span className="ml-1 font-mono text-xs">({e.reason})</span>}
        </span>
      </div>
      {answer && (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted max-md:py-3">
            {t('history.response')}
          </summary>
          <pre
            ref={pre}
            className="mt-1 overflow-x-auto rounded bg-surface-raised p-2 font-mono wrap-anywhere text-xs whitespace-pre-wrap"
          >
            <ForeignText text={answer} />
          </pre>
          <div className="mt-1 flex items-center gap-3">
            <CopyButton text={answer} target={pre} label={t('history.copy')} />
            {e.truncated && <span className="text-xs text-warn">{t('history.truncated')}</span>}
          </div>
        </details>
      )}
    </li>
  );
}

// HistoryTab lists the commands of the server, newest first (Design 6.3);
// in R1 moderator and supporter see only their own, which the backend
// decides.
export function HistoryTab() {
  const { t } = useTranslation();
  const { data: session } = useSession();
  const server = useServer();
  const history = useHistory(server?.id ?? '');
  const entries = history.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section aria-label={t('history.title')} className="flex flex-col gap-3">
      {!isAdmin(session?.user.role) && <p className="text-sm text-muted">{t('history.ownOnly')}</p>}
      {history.isPending && <p className="text-muted">{t('app.loading')}</p>}
      {history.isError && <p className="text-danger">{t('app.error')}</p>}
      {history.isSuccess && entries.length === 0 && (
        <p className="text-muted">{t('history.empty')}</p>
      )}
      {entries.length > 0 && (
        <ol className="rounded-lg border border-border bg-surface">
          {entries.map((e) => (
            <Entry key={e.request_id} e={e} />
          ))}
        </ol>
      )}
      {history.hasNextPage && (
        <Button
          variant="outline"
          className="self-start"
          disabled={history.isFetchingNextPage}
          onClick={() => void history.fetchNextPage()}
        >
          {t('history.more')}
        </Button>
      )}
    </section>
  );
}
