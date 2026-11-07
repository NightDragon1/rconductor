// SPDX-License-Identifier: AGPL-3.0-or-later

// LiveSocket is the one WebSocket of a browser tab (Design 2.1, 2.3): it
// subscribes topics for their listeners, confirms them with subscribed,
// reconnects with backoff and subscribes again after a reconnect.

export interface LiveMessage {
  type: 'subscribed' | 'event' | 'error';
  topic?: string;
  data?: unknown;
  reason?: string;
}

type Listener = (m: LiveMessage) => void;

export function liveURL(): string {
  const u = new URL('api/v1/ws', document.baseURI);
  u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
  return u.toString();
}

export class LiveSocket {
  private ws?: WebSocket;
  private readonly topics = new Map<string, Set<Listener>>();
  private attempt = 0;
  private timer?: ReturnType<typeof setTimeout>;
  private closed = false;

  constructor(
    private readonly url: string = liveURL(),
    private readonly onReconnect: () => void = () => {},
  ) {}

  // subscribe also opens a closed socket again: a provider that unmounts
  // and mounts again (React StrictMode) keeps working.
  subscribe(topic: string, listener: Listener): () => void {
    this.closed = false;
    let set = this.topics.get(topic);
    if (!set) {
      set = new Set();
      this.topics.set(topic, set);
      this.send({ type: 'subscribe', topic });
    }
    set.add(listener);
    this.open();
    return () => {
      const s = this.topics.get(topic);
      if (!s) return;
      s.delete(listener);
      if (s.size === 0) {
        this.topics.delete(topic);
        this.send({ type: 'unsubscribe', topic });
      }
    };
  }

  close() {
    this.closed = true;
    clearTimeout(this.timer);
    this.ws?.close();
  }

  private open() {
    if (this.ws || this.closed) return;
    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.onopen = () => {
      const reconnect = this.attempt > 0;
      this.attempt = 0;
      for (const topic of this.topics.keys()) {
        ws.send(JSON.stringify({ type: 'subscribe', topic }));
      }
      // After a reconnect the current state comes per REST (Design 2.3).
      if (reconnect) this.onReconnect();
    };
    ws.onmessage = (ev) => {
      let m: LiveMessage;
      try {
        m = JSON.parse(String(ev.data)) as LiveMessage;
      } catch {
        return;
      }
      const listeners = m.topic ? this.topics.get(m.topic) : undefined;
      if (listeners) {
        for (const l of listeners) l(m);
      } else if (m.type === 'error') {
        // A socket-wide error (replaced, session ended): every listener.
        for (const set of this.topics.values()) for (const l of set) l(m);
      }
    };
    ws.onclose = () => {
      this.ws = undefined;
      if (this.closed || this.topics.size === 0) return;
      const wait = Math.min(1000 * 1.6 ** this.attempt, 30_000) * (0.8 + Math.random() * 0.4);
      this.attempt++;
      this.timer = setTimeout(() => this.open(), wait);
    };
  }

  private send(m: { type: string; topic: string }) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(m));
    }
  }
}
