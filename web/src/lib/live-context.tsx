// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { LiveSocket, type LiveMessage } from './live';

const LiveContext = createContext<LiveSocket | null>(null);

// LiveProvider holds the one WebSocket of the tab; after a reconnect the
// current state comes again per REST (Design 2.3).
export function LiveProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [socket] = useState(() => new LiveSocket(undefined, () => void qc.invalidateQueries()));
  useEffect(() => () => socket.close(), [socket]);
  return <LiveContext.Provider value={socket}>{children}</LiveContext.Provider>;
}

// useTopic subscribes a topic while the component is mounted. Without a
// provider (unit tests) it does nothing.
export function useTopic(topic: string | undefined, listener: (m: LiveMessage) => void) {
  const socket = useContext(LiveContext);
  const ref = useRef(listener);
  useEffect(() => {
    ref.current = listener;
  });
  useEffect(() => {
    if (!socket || !topic) return;
    return socket.subscribe(topic, (m) => ref.current(m));
  }, [socket, topic]);
}
