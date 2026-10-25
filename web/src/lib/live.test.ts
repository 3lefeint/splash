import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { Live, STALE_AFTER_MS, type EventSourceLike } from './live.svelte';
import type { KeyValueStore } from './storage';
import type { State } from './types';

class FakeSource implements EventSourceLike {
  onerror: ((e: unknown) => void) | null = null;
  onopen: ((e: unknown) => void) | null = null;
  closed = false;
  listeners: Record<string, (e: { data: string }) => void> = {};
  addEventListener(type: string, l: (e: { data: string }) => void) {
    this.listeners[type] = l;
  }
  close() {
    this.closed = true;
  }
  emit(data: string) {
    this.listeners['state']({ data });
  }
}

const mem = (): KeyValueStore & { data: Record<string, string> } => {
  const data: Record<string, string> = {};
  return { data, getItem: (k) => data[k] ?? null, setItem: (k, v) => void (data[k] = v) };
};

const state = (over: Partial<State> = {}): State => ({
  ready: true, et: 1000, time_left: 500, opponents: { ahead: null, behind: null },
  params: {} as State['params'], overrides: {}, ...over,
});

let clock = 0;
let src: FakeSource;
const make = (storage: KeyValueStore | null = mem()) =>
  new Live({ eventSource: () => (src = new FakeSource()), storage, now: () => clock });

beforeEach(() => {
  clock = 10_000;
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

describe('Live', () => {
  it('shows the state of a message and is live', () => {
    const live = make();
    live.start();
    src.emit(JSON.stringify(state({ et: 1234 })));
    expect(live.status).toBe('live');
    expect(live.state?.et).toBe(1234);
    live.stop();
  });

  it('advances game time between messages', () => {
    const live = make();
    live.start();
    src.emit(JSON.stringify(state({ et: 1000 })));
    clock += 2500;
    live.tick();
    expect(live.etNow).toBeCloseTo(1002.5);
    live.stop();
  });

  it('turns stale when messages stop and live again with the next one', () => {
    const live = make();
    live.start();
    src.emit(JSON.stringify(state()));
    clock += STALE_AFTER_MS + 1;
    live.tick();
    expect(live.status).toBe('stale');
    src.emit(JSON.stringify(state()));
    expect(live.status).toBe('live');
    live.stop();
  });

  it('goes offline on a stream error and ignores damaged messages', () => {
    const live = make();
    live.start();
    src.emit(JSON.stringify(state({ et: 7 })));
    src.emit('{not json');
    expect(live.state?.et).toBe(7);
    src.onerror?.(new Error('x'));
    expect(live.status).toBe('offline');
    live.stop();
  });

  it('restores the last ready state from the cache as offline', () => {
    const store = mem();
    const first = make(store);
    first.start();
    src.emit(JSON.stringify(state({ et: 4242 })));
    first.stop();
    clock += 60_000;
    const second = make(store);
    expect(second.state?.et).toBe(4242);
    expect(second.status).toBe('offline');
    expect(second.fromCache).toBe(true);
    second.start();
    src.emit(JSON.stringify(state({ et: 5000 })));
    expect(second.fromCache).toBe(false);
    second.stop();
  });

  it('does not cache a state that is not ready, and works without storage', () => {
    const store = mem();
    const live = make(store);
    live.start();
    src.emit(JSON.stringify(state({ ready: false })));
    expect(store.data['splash.lastState']).toBeUndefined();
    live.stop();
    const bare = make(null);
    bare.start();
    src.emit(JSON.stringify(state()));
    expect(bare.status).toBe('live');
    bare.stop();
  });

  it('stop closes the stream and start is idempotent', () => {
    const live = make();
    live.start();
    const first = src;
    live.start();
    expect(src).toBe(first);
    live.stop();
    expect(first.closed).toBe(true);
  });
});
