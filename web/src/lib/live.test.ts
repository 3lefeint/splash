import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { Live, probeAuth, reconnectDelay, STALE_AFTER_MS, type AuthProbe, type EventSourceLike } from './live.svelte';
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
  ping(data = '15000') {
    this.listeners['ping']({ data });
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

describe('reconnecting', () => {
  let sources: FakeSource[];
  let verdict: AuthProbe;
  const reconnecting = (random = () => 0) => {
    sources = [];
    return new Live({
      eventSource: () => {
        const s = new FakeSource();
        sources.push(s);
        return s;
      },
      storage: mem(),
      now: () => clock,
      probe: async () => verdict,
      random,
    });
  };
  const lose = async (live: Live, ms: number) => {
    sources[sources.length - 1].onerror?.(new Error('cut'));
    await vi.advanceTimersByTimeAsync(ms);
    return live;
  };

  beforeEach(() => {
    verdict = 'ok';
  });

  it('computes pauses that double up to 30 s with jitter between half and the full step', () => {
    expect([0, 1, 2, 3].map((n) => reconnectDelay(n, () => 1))).toEqual([1000, 2000, 4000, 8000]);
    expect([0, 1, 2].map((n) => reconnectDelay(n, () => 0))).toEqual([500, 1000, 2000]);
    expect(reconnectDelay(20, () => 1)).toBe(30_000);
    expect(reconnectDelay(20, () => 0)).toBe(15_000);
  });

  it('keeps the last state, says it is reconnecting, and opens a new stream after a growing pause', async () => {
    const live = reconnecting();
    live.start();
    sources[0].emit(JSON.stringify(state({ et: 777 })));
    await lose(live, 0);
    expect(sources[0].closed).toBe(true);
    expect(live.status).toBe('offline');
    expect(live.reconnecting).toBe(true);
    expect(live.state?.et).toBe(777); // still shown
    expect(sources).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(499);
    expect(sources).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(sources).toHaveLength(2); // 500 ms: the shortest pause

    await lose(live, 999); // the new stream fails too, the pause doubles
    expect(sources).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(sources).toHaveLength(3);
    live.stop();
  });

  it('resumes with the next message and starts the pauses over', async () => {
    const live = reconnecting();
    live.start();
    sources[0].emit(JSON.stringify(state()));
    await lose(live, 500);
    await lose(live, 1000);
    expect(sources).toHaveLength(3);
    sources[2].emit(JSON.stringify(state({ et: 2000 })));
    expect(live.status).toBe('live');
    expect(live.reconnecting).toBe(false);
    expect(live.state?.et).toBe(2000);
    await lose(live, 499);
    expect(sources).toHaveLength(3);
    await vi.advanceTimersByTimeAsync(1);
    expect(sources).toHaveLength(4); // back to the first step
    live.stop();
  });

  it('never waits longer than 30 s', async () => {
    const live = reconnecting(() => 1);
    live.start();
    for (let i = 0; i < 8; i++) await lose(live, 30_000);
    const before = sources.length;
    await lose(live, 29_999);
    expect(sources).toHaveLength(before);
    await vi.advanceTimersByTimeAsync(1);
    expect(sources).toHaveLength(before + 1);
    live.stop();
  });

  it('stops for good and asks for a new login when the session has expired', async () => {
    const live = reconnecting();
    live.start();
    sources[0].emit(JSON.stringify(state({ et: 9 })));
    verdict = 'expired';
    await lose(live, 0);
    expect(live.authProblem).toBe('expired');
    expect(live.reconnecting).toBe(false);
    expect(live.state?.et).toBe(9); // the last state stays visible
    await vi.advanceTimersByTimeAsync(10 * 60_000);
    expect(sources).toHaveLength(1); // no loop
    live.stop();
  });

  it('does not loop for an account without a team either', async () => {
    const live = reconnecting();
    live.start();
    verdict = 'denied';
    await lose(live, 0);
    expect(live.authProblem).toBe('denied');
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sources).toHaveLength(1);
    live.stop();
  });

  it('treats a stream that goes silent without an error as lost, and pings keep it alive', async () => {
    const live = reconnecting();
    live.start();
    sources[0].emit(JSON.stringify(state()));
    for (let i = 0; i < 6; i++) {
      clock += 20_000;
      sources[0].ping('15000'); // a quiet but healthy connection
      live.tick();
    }
    expect(sources[0].closed).toBe(false);
    clock += 45_001; // three intervals without a sign of life
    live.tick();
    await vi.advanceTimersByTimeAsync(0);
    expect(sources[0].closed).toBe(true);
    expect(live.reconnecting).toBe(true);
    await vi.advanceTimersByTimeAsync(500);
    expect(sources).toHaveLength(2);
    live.stop();
  });

  it('stop cancels a pending reconnect', async () => {
    const live = reconnecting();
    live.start();
    await lose(live, 100);
    live.stop();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sources).toHaveLength(1);
  });
});

describe('probeAuth', () => {
  afterEach(() => vi.unstubAllGlobals());
  const answer = (r: Partial<Response> & { headers?: Headers }) =>
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, redirected: false, type: 'basic', headers: new Headers({ 'content-type': 'application/json' }), ...r })));

  it('recognises a working session', async () => {
    answer({});
    expect(await probeAuth()).toBe('ok');
  });
  it('recognises an expired session by 401, redirect or a login page', async () => {
    answer({ ok: false, status: 401 });
    expect(await probeAuth()).toBe('expired');
    answer({ ok: false, status: 0, type: 'opaqueredirect' });
    expect(await probeAuth()).toBe('expired');
    answer({ redirected: true });
    expect(await probeAuth()).toBe('expired');
    answer({ headers: new Headers({ 'content-type': 'text/html' }) });
    expect(await probeAuth()).toBe('expired');
  });
  it('tells a missing team from an outage', async () => {
    answer({ ok: false, status: 403 });
    expect(await probeAuth()).toBe('denied');
    answer({ ok: false, status: 502 });
    expect(await probeAuth()).toBe('down');
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('network'))));
    expect(await probeAuth()).toBe('down');
  });
});
