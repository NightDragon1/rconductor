// SPDX-License-Identifier: AGPL-3.0-or-later
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from 'react';
import { useBlocker } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import type { Outcome } from '@/api/users';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { reasonText } from '@/lib/admin';

// UNDO_MS is how long Undo stays offered after a save (UI-008, D-048).
export const UNDO_MS = 5000;

interface Note {
  id: number;
  label: string;
  // revert sends the previous state as a new change; without it the note
  // only tells that the change is saved.
  revert?: () => void;
  timer: ReturnType<typeof setTimeout>;
  until: number;
}

interface Undo {
  saved: (label: string, revert?: () => void) => void;
  undoLast: () => void;
  // guard runs leave at once, or after a question while something is
  // unsaved; for what leaves a panel without leaving the page.
  guard: (leave: () => void) => void;
  // saving marks a save in flight until its outcome is there: leaving
  // waits for it instead of asking (UI-008).
  saving: (p: Promise<Outcome>) => Promise<Outcome>;
  dirty?: RefObject<Set<symbol>>;
}

const UndoContext = createContext<Undo>({
  saved: () => undefined,
  undoLast: () => undefined,
  guard: (leave) => leave(),
  saving: (p) => p,
});

// UndoProvider shows "Saved" with Undo after a change (Design 6.1) and asks
// before unsaved changes are left, inside the panel or by navigation.
export function UndoProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [notes, setNotes] = useState<Note[]>([]);
  const ref = useRef<Note[]>([]);
  useEffect(() => {
    ref.current = notes;
  }, [notes]);
  const [now, setNow] = useState(() => Date.now());
  const seq = useRef(0);
  const dirty = useRef(new Set<symbol>());
  const [leaving, setLeaving] = useState<(() => void) | null>(null);
  const inFlight = useRef(new Set<Promise<Outcome>>());
  // waiting hides the question while a leave waits for a save; error is
  // the refusal of that save, shown in the question.
  const [waiting, setWaiting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const turn = useRef(0);
  const blocker = useBlocker({
    shouldBlockFn: () => {
      if (inFlight.current.size > 0) {
        setWaiting(true);
        return true;
      }
      return dirty.current.size > 0;
    },
    enableBeforeUnload: () => dirty.current.size > 0 || inFlight.current.size > 0,
    withResolver: true,
  });

  const saving = useCallback((p: Promise<Outcome>) => {
    const tracked = p.catch((): Outcome => ({ id: '', ok: false, reason: 'error' }));
    inFlight.current.add(tracked);
    void tracked.then(() => inFlight.current.delete(tracked));
    return tracked;
  }, []);
  // settle waits for every save in flight, also those started meanwhile;
  // the first refusal is the result.
  const settle = useCallback(async () => {
    let failed: Outcome | undefined;
    while (inFlight.current.size > 0) {
      const batch = [...inFlight.current];
      const results = await Promise.all(batch);
      batch.forEach((b) => inFlight.current.delete(b));
      failed ??= results.find((r) => !r.ok);
    }
    return failed;
  }, []);
  // after runs go once the saves are done, or asks with the refusal. go
  // runs after the render that shows what the save changed.
  const next = useRef<(() => void) | null>(null);
  const [settled, setSettled] = useState(0);
  useEffect(() => {
    const go = next.current;
    next.current = null;
    go?.();
  }, [settled]);
  const after = useCallback(
    (go: () => void, ask: () => void) => {
      const mine = ++turn.current;
      void settle().then((failed) => {
        if (mine !== turn.current) return;
        setWaiting(false);
        if (failed) {
          setError(reasonText(t, failed));
          ask();
          return;
        }
        next.current = go;
        setSettled((n) => n + 1);
      });
    },
    [settle, t],
  );

  const drop = useCallback((id: number) => setNotes((n) => n.filter((x) => x.id !== id)), []);
  const saved = useCallback(
    (label: string, revert?: () => void) => {
      const id = ++seq.current;
      const timer = setTimeout(() => drop(id), UNDO_MS);
      setNotes((n) => [...n, { id, label, revert, timer, until: Date.now() + UNDO_MS }]);
    },
    [drop],
  );
  const undo = useCallback(
    (id: number) => {
      const n = ref.current.find((x) => x.id === id);
      if (n) {
        clearTimeout(n.timer);
        n.revert?.();
      }
      drop(id);
    },
    [drop],
  );
  const undoLast = useCallback(() => {
    const last = ref.current.findLast((x) => x.revert);
    if (last) undo(last.id);
  }, [undo]);
  const guard = useCallback(
    (leave: () => void) => {
      if (inFlight.current.size > 0) {
        setWaiting(true);
        after(leave, () => setLeaving(() => leave));
      } else if (dirty.current.size > 0) setLeaving(() => leave);
      else leave();
    },
    [after],
  );
  // A navigation held while a save runs goes on once it is saved.
  const { status, proceed } = blocker;
  useEffect(() => {
    if (status !== 'blocked' || !waiting) return;
    after(
      () => proceed?.(),
      () => undefined,
    );
  }, [status, waiting, proceed, after]);

  useEffect(() => {
    if (notes.length === 0) return;
    const i = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(i);
  }, [notes.length]);

  return (
    <UndoContext.Provider value={{ saved, undoLast, guard, saving, dirty }}>
      {children}
      <ConfirmDialog
        open={!waiting && (leaving !== null || blocker.status === 'blocked')}
        title={t('undo.leaveTitle')}
        text={t('undo.leaveText')}
        action={t('undo.leave')}
        onClose={() => {
          setLeaving(null);
          setError(null);
          blocker.reset?.();
        }}
        onConfirm={() => {
          dirty.current.clear();
          leaving?.();
          setLeaving(null);
          setError(null);
          blocker.proceed?.();
        }}
      >
        {error && (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        )}
      </ConfirmDialog>
      {notes.length > 0 && (
        <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 flex-col gap-2">
          {notes.map((n) => (
            <div
              key={n.id}
              role="status"
              className="flex items-center gap-4 rounded-lg border border-border bg-surface-raised px-4 py-3 text-sm shadow-lg"
            >
              <span>{t('undo.saved', { what: n.label })}</span>
              {n.revert && (
                <>
                  <button
                    type="button"
                    className="tap font-semibold text-accent"
                    onClick={() => undo(n.id)}
                  >
                    {t('undo.undo')}
                  </button>
                  <span className="font-mono text-xs text-muted">
                    {t('undo.seconds', { s: Math.max(0, Math.ceil((n.until - now) / 1000)) })}
                  </span>
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </UndoContext.Provider>
  );
}

export const useUndo = () => useContext(UndoContext);

// useUnsaved marks unsaved changes while on is true (UI-008).
export function useUnsaved(on: boolean) {
  const { dirty } = useUndo();
  useEffect(() => {
    if (!on || !dirty) return;
    const set = dirty.current;
    const key = Symbol();
    set.add(key);
    return () => {
      set.delete(key);
    };
  }, [on, dirty]);
}
