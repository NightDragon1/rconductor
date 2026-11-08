// SPDX-License-Identifier: AGPL-3.0-or-later
import { useId, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { usePlayers } from '@/api/servers';
import { useDeleteTemplate, useSaveTemplate, useTemplates } from '@/api/templates';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { reasonText } from '@/lib/admin';
import type { Values } from '@/lib/commandform';
import { stripCodes } from '@/lib/serverpage';
import { matchesTemplate, type Template } from '@/lib/templates';
import { TemplateCard } from './CommandsTab';

function refusal(t: (k: string) => string, e: unknown): string {
  return reasonText(t, e instanceof ApiError ? e.problem : undefined);
}

// TemplateEditor creates a template or changes one (CMD-008).
function TemplateEditor({
  serverId,
  from,
  onDone,
}: {
  serverId: string;
  from?: Template;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const save = useSaveTemplate(serverId);
  const [name, setName] = useState(from?.name ?? '');
  const [text, setText] = useState(from?.template ?? '');
  const nameId = useId();
  const textId = useId();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ from, name, template: text }, { onSuccess: onDone });
  };
  return (
    <form
      onSubmit={submit}
      aria-label={from ? t('templates.edit') : t('templates.new')}
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4"
    >
      <div className="flex flex-col gap-1">
        <label htmlFor={nameId} className="text-sm text-muted">
          {t('templates.name')}
        </label>
        <Input id={nameId} maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor={textId} className="text-sm text-muted">
          {t('templates.text')}
        </label>
        <Input
          id={textId}
          className="font-mono"
          autoComplete="off"
          spellCheck={false}
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <p className="text-xs text-muted">{t('templates.textHelp')}</p>
      </div>
      {save.isError && (
        <p role="alert" className="text-sm text-danger">
          {refusal(t, save.error)}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={!name.trim() || !text.trim() || save.isPending}>
          {t('templates.save')}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t('actions.cancel')}
        </Button>
      </div>
    </form>
  );
}

// TemplatesPanel is the templates of the console for admins (CMD-008,
// D-068): fill one to put the command into the input line, or manage
// them; no tab of its own (Design 6.3).
export function TemplatesPanel({
  serverId,
  isPlayer,
  open,
  onClose,
  onUse,
}: {
  serverId: string;
  isPlayer: (name: string) => boolean;
  open: boolean;
  onClose: () => void;
  onUse: (command: string) => void;
}) {
  const { t } = useTranslation();
  const templates = useTemplates(serverId, open);
  const players = usePlayers(serverId);
  const remove = useDeleteTemplate(serverId);
  const listId = useId();
  const [values, setValues] = useState<Record<string, Values>>({});
  const [editing, setEditing] = useState<Template | 'new' | null>(null);
  const [deleting, setDeleting] = useState<Template | null>(null);
  const [search, setSearch] = useState('');
  // Color codes are no part of a name (SEC-006).
  const names = (players.data?.players ?? []).map(stripCodes);
  const shown = (templates.data ?? []).filter((x) => matchesTemplate(x, search));
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogTitle>{t('templates.title')}</DialogTitle>
        <DialogDescription>{t('templates.explain')}</DialogDescription>
        <datalist id={listId}>
          {names.map((n) => (
            <option key={n} value={n} />
          ))}
        </datalist>
        <div className="flex flex-wrap gap-2">
          <Input
            type="search"
            className="min-w-0 flex-1"
            placeholder={t('templates.search')}
            aria-label={t('templates.search')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Button variant="outline" onClick={() => setEditing('new')}>
            {t('templates.new')}
          </Button>
        </div>
        {editing && (
          <TemplateEditor
            key={editing === 'new' ? 'new' : editing.id}
            serverId={serverId}
            from={editing === 'new' ? undefined : editing}
            onDone={() => setEditing(null)}
          />
        )}
        {templates.isPending && <p className="text-muted">{t('app.loading')}</p>}
        {templates.isSuccess && templates.data.length === 0 && (
          <p className="text-muted">{t('templates.none')}</p>
        )}
        {remove.isError && (
          <p role="alert" className="text-sm text-danger">
            {refusal(t, remove.error)}
          </p>
        )}
        <div className="flex flex-col gap-3">
          {shown.map((x) => (
            <TemplateCard
              key={x.id}
              tmpl={x}
              values={values[x.id] ?? {}}
              players={names}
              listId={listId}
              busy={false}
              isPlayer={isPlayer}
              action={t('templates.use')}
              onChange={(name, v) =>
                setValues((all) => ({ ...all, [x.id]: { ...all[x.id], [name]: v } }))
              }
              onFocusPlayer={() => undefined}
              onRun={(command) => {
                onUse(command);
                onClose();
              }}
            >
              <div className="flex shrink-0 gap-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label={t('templates.editName', { name: x.name })}
                  onClick={() => setEditing(x)}
                >
                  {t('templates.edit')}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label={t('templates.deleteName', { name: x.name })}
                  onClick={() => setDeleting(x)}
                >
                  {t('templates.delete')}
                </Button>
              </div>
            </TemplateCard>
          ))}
        </div>
        <ConfirmDialog
          open={!!deleting}
          title={t('templates.deleteTitle', { name: deleting?.name ?? '' })}
          text={t('templates.deleteText')}
          action={t('templates.delete')}
          onConfirm={() => {
            if (deleting) remove.mutate(deleting.id);
            setDeleting(null);
          }}
          onClose={() => setDeleting(null)}
        />
      </DialogContent>
    </Dialog>
  );
}
