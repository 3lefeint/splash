import { readJson, safeStorage, writeJson, type KeyValueStore } from './storage';
import type { State } from './types';

export type Status = 'connecting' | 'live' | 'stale' | 'offline';

export interface EventSourceLike {
  addEventListener(type: string, listener: (e: { data: string }) => void): void;
  onerror: ((e: unknown) => void) | null;
  onopen: ((e: unknown) => void) | null;
  close(): void;
}

export interface LiveDeps {
  eventSource: (url: string) => EventSourceLike;
  storage: KeyValueStore | null;
  now: () => number;
}

/** No state for this long means the stream is alive but the agents are silent. */
export const STALE_AFTER_MS = 5000;
const CACHE_KEY = 'splash.lastState';
const TICK_MS = 250;

interface Cached {
  savedAt: number;
  state: State;
}

/** The newest state from the SSE stream, with connection status and a clock for countdowns. */
export class Live {
  state = $state<State | null>(null);
  status = $state<Status>('connecting');
  /** True while the shown state comes from the browser cache, not from a live message. */
  fromCache = $state(false);
  nowMs = $state(0);
  receivedAt = $state(0);

  private source: EventSourceLike | null = null;
  private timer: ReturnType<typeof setInterval> | null = null;

  constructor(private deps: LiveDeps) {
    this.nowMs = deps.now();
    const cached = readJson<Cached>(deps.storage, CACHE_KEY);
    if (cached?.state) {
      this.state = cached.state;
      this.receivedAt = cached.savedAt;
      this.fromCache = true;
      this.status = 'offline';
    }
  }

  /** Game time now, advanced between the one-per-second messages. */
  get etNow(): number {
    if (!this.state) return 0;
    if (this.status !== 'live') return this.state.et;
    return this.state.et + Math.max(this.nowMs - this.receivedAt, 0) / 1000;
  }

  start(): void {
    if (this.source) return;
    const es = this.deps.eventSource('/api/stream');
    this.source = es;
    es.onopen = () => {
      if (this.status === 'offline' || this.status === 'connecting') this.status = this.fromCache ? 'offline' : 'stale';
    };
    es.addEventListener('state', (e) => this.receive(e.data));
    es.onerror = () => {
      this.status = 'offline';
    };
    this.timer = setInterval(() => this.tick(), TICK_MS);
  }

  stop(): void {
    this.source?.close();
    this.source = null;
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  receive(data: string): void {
    let state: State;
    try {
      state = JSON.parse(data) as State;
    } catch {
      return; // a damaged message must not break the display
    }
    this.nowMs = this.deps.now();
    this.receivedAt = this.nowMs;
    this.state = state;
    this.fromCache = false;
    this.status = 'live';
    if (state.ready) writeJson(this.deps.storage, CACHE_KEY, { savedAt: this.nowMs, state } satisfies Cached);
  }

  tick(): void {
    this.nowMs = this.deps.now();
    if (this.status === 'live' && this.nowMs - this.receivedAt > STALE_AFTER_MS) this.status = 'stale';
  }
}

export function createLive(): Live {
  return new Live({
    eventSource: (url) => new EventSource(url) as unknown as EventSourceLike,
    storage: safeStorage(),
    now: () => Date.now(),
  });
}
