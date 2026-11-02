// SPDX-License-Identifier: AGPL-3.0-or-later
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LiveSocket } from './live';

class FakeSocket {
  static last?: FakeSocket;
  static readonly OPEN = 1;
  readyState = 0;
  sent: string[] = [];
  onopen?: () => void;
  onmessage?: (e: { data: string }) => void;
  onclose?: () => void;
  constructor(readonly url: string) {
    FakeSocket.last = this;
  }
  send(s: string) {
    this.sent.push(s);
  }
  close() {
    this.onclose?.();
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
}

function last(): FakeSocket {
  if (!FakeSocket.last) throw new Error('no socket opened');
  return FakeSocket.last;
}

describe('LiveSocket', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('subscribes once per topic, delivers to listeners and subscribes again after a reconnect', () => {
    vi.useFakeTimers();
    vi.stubGlobal('WebSocket', FakeSocket);
    const reconnected = vi.fn();
    const live = new LiveSocket('ws://panel/api/v1/ws', reconnected);
    const a = vi.fn();
    const b = vi.fn();
    live.subscribe('servers.overview', a);
    live.subscribe('servers.overview', b);
    const first = last();
    first.open();
    expect(first.sent).toEqual([JSON.stringify({ type: 'subscribe', topic: 'servers.overview' })]);
    first.onmessage?.({
      data: JSON.stringify({ type: 'event', topic: 'servers.overview', data: 1 }),
    });
    expect(a).toHaveBeenCalledOnce();
    expect(b).toHaveBeenCalledOnce();

    first.close();
    vi.advanceTimersByTime(2000);
    const second = last();
    expect(second).not.toBe(first);
    second.open();
    expect(second.sent).toEqual([JSON.stringify({ type: 'subscribe', topic: 'servers.overview' })]);
    expect(reconnected).toHaveBeenCalledOnce();
    live.close();
    vi.useRealTimers();
  });
});
