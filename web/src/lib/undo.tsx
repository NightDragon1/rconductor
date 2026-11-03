// SPDX-License-Identifier: AGPL-3.0-or-later
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { useTranslation } from 'react-i18next';

// UNDO_MS is how long a change waits before it goes out (UI-008, F8).
export const UNDO_MS = 5000;

interface Pending {
  id: number;
  label: string;
  // send carries the change out; keepalive lets it outlive the page.
  send: (keepalive: boolean) => Promise<void>;
  timer: ReturnType<typeof setTimeout>;
  until: number;
}

interface Undo {
  schedule: (label: string, send: (keepalive: boolean) => Promise<void>) => void;
  undoLast: () => void;
}

const UndoContext = createContext<Undo>({
  schedule: (_l, send) => void send(false),
  undoLast: () => undefined,
});

// UndoProvider delays changes by UNDO_MS with an Undo note instead of a
// confirmation (Design 6.1); open changes go out at once when the page
// goes away (pagehide).
export function UndoProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [pending, setPending] = useState<Pending[]>([]);
  const ref = useRef<Pending[]>([]);
  useEffect(() => {
    ref.current = pending;
  }, [pending]);
  const [now, setNow] = useState(() => Date.now());
  const seq = useRef(0);

  const drop = useCallback((id: number) => setPending((p) => p.filter((x) => x.id !== id)), []);
  const schedule = useCallback(
    (label: string, send: (keepalive: boolean) => Promise<void>) => {
      const id = ++seq.current;
      const timer = setTimeout(() => {
        drop(id);
        void send(false);
      }, UNDO_MS);
      setPending((p) => [...p, { id, label, send, timer, until: Date.now() + UNDO_MS }]);
    },
    [drop],
  );
  const undo = useCallback(
    (id: number) => {
      const p = ref.current.find((x) => x.id === id);
      if (p) clearTimeout(p.timer);
      drop(id);
    },
    [drop],
  );
  const undoLast = useCallback(() => {
    const last = ref.current[ref.current.length - 1];
    if (last) undo(last.id);
  }, [undo]);

  useEffect(() => {
    const flush = () => {
      for (const p of ref.current) {
        clearTimeout(p.timer);
        void p.send(true);
      }
      ref.current = [];
    };
    window.addEventListener('pagehide', flush);
    return () => window.removeEventListener('pagehide', flush);
  }, []);
  useEffect(() => {
    if (pending.length === 0) return;
    const i = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(i);
  }, [pending.length]);

  return (
    <UndoContext.Provider value={{ schedule, undoLast }}>
      {children}
      {pending.length > 0 && (
        <div className="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 flex-col gap-2">
          {pending.map((p) => (
            <div
              key={p.id}
              role="status"
              className="flex items-center gap-4 rounded-lg border border-border bg-surface-raised px-4 py-3 text-sm shadow-lg"
            >
              <span>{p.label}</span>
              <button
                type="button"
                className="font-semibold text-accent"
                onClick={() => undo(p.id)}
              >
                {t('undo.undo')}
              </button>
              <span className="font-mono text-xs text-muted">
                {t('undo.seconds', { s: Math.max(0, Math.ceil((p.until - now) / 1000)) })}
              </span>
            </div>
          ))}
        </div>
      )}
    </UndoContext.Provider>
  );
}

export const useUndo = () => useContext(UndoContext);
