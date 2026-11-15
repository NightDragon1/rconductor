// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, must, putSayPrefix } from '@/api/client';
import type { components } from '@/api/schema';
import type { Server, ServerGroup } from '@/api/servers';
import { outcome } from '@/api/users';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import { prefixProblem, winningGroup } from '@/lib/sayprefix';
import { useUndo, useUnsaved } from '@/lib/undo';

type SayPrefix = components['schemas']['SayPrefix'];
type Message = { ok: boolean; text: string } | null;

// Below the key of the server groups: a change of the groups reads the
// prefixes again.
const sayPrefixKey = (id: string) => ['server-groups', id, 'say-prefix'] as const;

// sayPrefixQuery reads the prefix of a group (CMD-020, D-082). 403 and
// 404 (flag off, group outside the scope, role without the right) are an
// answer, not a fault: the field is not shown.
const sayPrefixQuery = (id: string) => ({
  queryKey: sayPrefixKey(id),
  queryFn: async (): Promise<SayPrefix | null> => {
    const r = await api.GET('/server-groups/{id}/say-prefix', { params: { path: { id } } });
    if (r.response.status === 403 || r.response.status === 404) return null;
    return must(r);
  },
});

// SayPrefixField sets the prefix of messages to the players for the
// servers of a group (CMD-020, D-082). It shows only when the backend
// answers the read; the backend checks the write again. Read-only on a
// phone (UI-012).
export function SayPrefixField({
  group,
  groups,
  servers,
  readOnly,
}: {
  group: ServerGroup;
  groups: ServerGroup[];
  servers: Server[];
  readOnly: boolean;
}) {
  const prefix = useQuery(sayPrefixQuery(group.id));
  const [message, setMessage] = useState<Message>(null);
  if (!prefix.data) return null;
  return (
    <div className="flex flex-col gap-2 border-t border-border pt-3">
      <Editor
        // A new version (saved, reloaded) starts the draft afresh.
        key={prefix.data.version}
        group={group}
        data={prefix.data}
        readOnly={readOnly}
        message={message}
        setMessage={setMessage}
        reload={async () => {
          await prefix.refetch();
          setMessage(null);
        }}
      />
      <SeveralGroups group={group} groups={groups} servers={servers} />
    </div>
  );
}

function Editor({
  group,
  data,
  readOnly,
  message,
  setMessage,
  reload,
}: {
  group: ServerGroup;
  data: SayPrefix;
  readOnly: boolean;
  message: Message;
  setMessage: (m: Message) => void;
  reload: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { saved, saving } = useUndo();
  const [draft, setDraft] = useState(data.say_prefix);
  const [conflict, setConflict] = useState(false);
  const inputId = useId();
  const hintId = useId();
  const dirty = draft !== data.say_prefix;
  useUnsaved(dirty);
  const problem = prefixProblem(draft);

  const write = async (version: string, value: string) => {
    let done = null as SayPrefix | null;
    const r = await saving(
      outcome(group.id, async () => {
        done = must(await putSayPrefix(group.id, version, { say_prefix: value }));
      }),
    );
    if (r.reason === 'conflict') {
      setConflict(true);
      setMessage({ ok: false, text: t('admin.conflict') });
      return null;
    }
    if (!r.ok) {
      setMessage({
        ok: false,
        text: r.reason === 'invalid' ? t('servers.sayPrefix.invalid') : reasonText(t, r.reason),
      });
      return null;
    }
    setMessage({
      ok: true,
      text: t(value ? 'servers.sayPrefix.saved' : 'servers.sayPrefix.cleared'),
    });
    if (done) qc.setQueryData(sayPrefixKey(group.id), done);
    return done as SayPrefix | null;
  };
  const save = async (value: string) => {
    const before = data.say_prefix;
    const after = await write(data.version, value);
    if (after) {
      saved(
        t('servers.sayPrefix.undo', { name: group.name }),
        () => void write(after.version, before),
      );
    }
  };

  return (
    <>
      <label htmlFor={inputId} className="text-sm text-muted">
        {t('servers.sayPrefix.label')}
      </label>
      <Input
        id={inputId}
        className="font-mono"
        value={draft}
        placeholder={t('servers.sayPrefix.placeholder')}
        aria-describedby={hintId}
        readOnly={readOnly}
        disabled={conflict}
        onChange={(e) => {
          setMessage(null);
          setDraft(e.target.value);
        }}
      />
      <p id={hintId} className={problem ? 'text-xs text-warn' : 'text-xs text-muted'}>
        {t(problem ? 'servers.sayPrefix.problem' : 'servers.sayPrefix.hint')}
      </p>
      {message && (
        <p
          role={message.ok ? 'status' : 'alert'}
          className={message.ok ? 'text-sm text-ok' : 'text-sm text-danger'}
        >
          {message.text}
        </p>
      )}
      {!readOnly && !conflict && (
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={!dirty} onClick={() => void save(draft)}>
            {t('servers.sayPrefix.save')}
          </Button>
          {data.say_prefix !== '' && (
            <Button variant="outline" onClick={() => void save('')}>
              {t('servers.sayPrefix.clear')}
            </Button>
          )}
        </div>
      )}
      {!readOnly && conflict && (
        <Button variant="outline" className="self-start" onClick={() => void reload()}>
          {t('admin.reload')}
        </Button>
      )}
    </>
  );
}

// SeveralGroups names, for each server of the group that is in other
// groups too, which group's prefix it uses: the first by name in byte
// order, then by ID, that has one (D-082). A group whose prefix the caller
// cannot read leaves the answer open; then only the rule shows.
function SeveralGroups({
  group,
  groups,
  servers,
}: {
  group: ServerGroup;
  groups: ServerGroup[];
  servers: Server[];
}) {
  const { t } = useTranslation();
  const shared = group.server_ids.filter((id) =>
    groups.some((g) => g.id !== group.id && g.server_ids.includes(id)),
  );
  const others = groups.filter(
    (g) => g.id !== group.id && g.server_ids.some((id) => shared.includes(id)),
  );
  const reads = useQueries({ queries: [group, ...others].map((g) => sayPrefixQuery(g.id)) });
  if (shared.length === 0) return null;
  const prefixes: Record<string, string | undefined> = {};
  [group, ...others].forEach((g, i) => {
    prefixes[g.id] = reads[i]?.data?.say_prefix;
  });
  const name = (id: string) => servers.find((s) => s.id === id)?.name ?? t('admin.unknownServer');
  const lines = shared.flatMap((id) => {
    const w = winningGroup(id, groups, prefixes);
    if (w === undefined) return [];
    return [
      {
        id,
        text: w
          ? t('servers.sayPrefix.uses', { server: name(id), group: w.name })
          : t('servers.sayPrefix.usesNone', { server: name(id) }),
      },
    ];
  });
  return (
    <div className="flex flex-col gap-1 text-xs text-muted" data-testid="say-prefix-several">
      <p>{t('servers.sayPrefix.several')}</p>
      {lines.length > 0 && (
        <ul className="flex flex-col gap-0.5">
          {lines.map((l) => (
            <li key={l.id}>{l.text}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
