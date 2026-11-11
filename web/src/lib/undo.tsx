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
import { ConfirmDialog } from '@/components/ConfirmDialog';

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
  dirty?: RefObject<Set<symbol>>;
  // released tells that an unsaved change is gone, saved or dropped.
  released?: () => void;
}

const UndoContext = createContext<Undo>({
  saved: () => undefined,
  undoLast: () => undefined,
  guard: (leave) => leave(),
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
  const [leaving, setLeavingState] = useState<(() => void) | null>(null);
  const leavingRef = useRef<(() => void) | null>(null);
  const setLeaving = useCallback((leave: (() => void) | null) => {
    leavingRef.current = leave;
    setLeavingState(() => leave);
  }, []);
  const blocker = useBlocker({
    shouldBlockFn: () => dirty.current.size > 0,
    enableBeforeUnload: () => dirty.current.size > 0,
    withResolver: true,
  });
  const blockerRef = useRef(blocker);
  useEffect(() => {
    blockerRef.current = blocker;
  }, [blocker]);
  // Once nothing is unsaved any more, an open question has no ground:
  // the leave that waits for it goes ahead.
  const released = useCallback(() => {
    if (dirty.current.size > 0) return;
    const leave = leavingRef.current;
    if (leave) {
      setLeaving(null);
      leave();
    }
    if (blockerRef.current.status === 'blocked') blockerRef.current.proceed();
  }, [setLeaving]);

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
      if (dirty.current.size > 0) setLeaving(leave);
      else leave();
    },
    [setLeaving],
  );

  useEffect(() => {
    if (notes.length === 0) return;
    const i = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(i);
  }, [notes.length]);

  return (
    <UndoContext.Provider value={{ saved, undoLast, guard, dirty, released }}>
      {children}
      <ConfirmDialog
        open={leaving !== null || blocker.status === 'blocked'}
        title={t('undo.leaveTitle')}
        text={t('undo.leaveText')}
        action={t('undo.leave')}
        onClose={() => {
          setLeaving(null);
          blocker.reset?.();
        }}
        onConfirm={() => {
          dirty.current.clear();
          leaving?.();
          setLeaving(null);
          blocker.proceed?.();
        }}
      />
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
  const { dirty, released } = useUndo();
  useEffect(() => {
    if (!on || !dirty) return;
    const set = dirty.current;
    const key = Symbol();
    set.add(key);
    return () => {
      set.delete(key);
      released?.();
    };
  }, [on, dirty, released]);
}
