import { readJson, safeStorage, writeJson, type KeyValueStore } from './storage';
import { normalizeState } from './normalize';
import type { State } from './types';

export type Status = 'connecting' | 'live' | 'stale' | 'offline';

export interface EventSourceLike {
  addEventListener(type: string, listener: (e: { data: string }) => void): void;
  onerror: ((e: unknown) => void) | null;
  onopen: ((e: unknown) => void) | null;
  close(): void;
}

/** What a look at the backend says about the login: fine, session over, not allowed, or just unreachable. */
export type AuthProbe = 'ok' | 'expired' | 'denied' | 'down';

export interface LiveDeps {
  eventSource: (url: string) => EventSourceLike;
  storage: KeyValueStore | null;
  now: () => number;
  /** Looks at the backend after the stream failed, to tell an expired session from a plain outage. */
  probe?: () => Promise<AuthProbe>;
  random?: () => number;
}

/** The pause before reconnect attempt n (0 based): doubles from one second up to 30 s, with jitter. */
export function reconnectDelay(attempt: number, random: () => number): number {
  const step = Math.min(BASE_DELAY_MS * 2 ** attempt, MAX_DELAY_MS);
  return step / 2 + (random() * step) / 2;
}

/** No state for this long means the stream is alive but the agents are silent. */
export const STALE_AFTER_MS = 5000;
const CACHE_KEY = 'splash.lastState';
const TICK_MS = 250;
const BASE_DELAY_MS = 1000;
const MAX_DELAY_MS = 30_000;
/** The backend pings an idle stream every 15 s by default; the connection counts as dead after three missed pings. */
const DEFAULT_PING_MS = 15_000;
const WATCHDOG_PINGS = 3;

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
  /** True from a lost stream until the next message: the shown state is the last known one. */
  reconnecting = $state(false);
  /** Why reconnecting stopped: the session is over (log in again) or the user may not use the app. */
  authProblem = $state<'expired' | 'denied' | null>(null);

  private source: EventSourceLike | null = null;
  private timer: ReturnType<typeof setInterval> | null = null;
  private retry: ReturnType<typeof setTimeout> | null = null;
  private running = false;
  private attempt = 0;
  private heardAt = 0;
  private pingMs = DEFAULT_PING_MS;

  constructor(private deps: LiveDeps) {
    this.nowMs = deps.now();
    const cached = readJson<Cached>(deps.storage, CACHE_KEY);
    if (cached?.state) {
      this.state = normalizeState(cached.state);
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
    if (this.running) return;
    this.running = true;
    this.open();
    this.timer = setInterval(() => this.tick(), TICK_MS);
  }

  stop(): void {
    this.running = false;
    this.source?.close();
    this.source = null;
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
    if (this.retry) clearTimeout(this.retry);
    this.retry = null;
  }

  private open(): void {
    const es = this.deps.eventSource('/api/stream');
    this.source = es;
    this.heardAt = this.deps.now();
    es.onopen = () => {
      if (this.status === 'offline' || this.status === 'connecting') this.status = this.fromCache ? 'offline' : 'stale';
    };
    es.addEventListener('state', (e) => this.receive(e.data));
    es.addEventListener('ping', (e) => this.ping(e.data));
    es.onerror = () => this.lost();
  }

  /** The stream failed or went silent: keep the last state, mark it, and come back with a pause that grows. */
  private lost(): void {
    this.status = 'offline';
    this.source?.close(); // the browser's own retry is fixed and stops for good on an HTTP error, so ours replaces it
    this.source = null;
    if (!this.running || this.retry) return;
    this.reconnecting = true;
    void this.afterLoss();
  }

  private async afterLoss(): Promise<void> {
    const verdict = await (this.deps.probe?.() ?? Promise.resolve<AuthProbe>('down'));
    if (!this.running) return;
    if (verdict === 'expired' || verdict === 'denied') {
      this.authProblem = verdict; // retrying cannot help and must not loop
      this.reconnecting = false;
      return;
    }
    const delay = reconnectDelay(this.attempt++, this.deps.random ?? Math.random);
    this.retry = setTimeout(() => {
      this.retry = null;
      if (this.running && !this.source) this.open();
    }, delay);
  }

  /** The backend's keep-alive: its data is the interval in milliseconds. */
  private ping(data: string): void {
    this.heardAt = this.deps.now();
    const ms = Number(data);
    if (Number.isFinite(ms) && ms > 0) this.pingMs = ms;
    this.connected();
  }

  private connected(): void {
    this.attempt = 0;
    this.reconnecting = false;
    this.authProblem = null;
  }

  receive(data: string): void {
    let state: State;
    try {
      state = normalizeState(JSON.parse(data) as State);
    } catch {
      return; // a damaged message must not break the display
    }
    this.nowMs = this.deps.now();
    this.receivedAt = this.nowMs;
    this.heardAt = this.nowMs;
    this.connected();
    this.state = state;
    this.fromCache = false;
    this.status = 'live';
    if (state.ready) writeJson(this.deps.storage, CACHE_KEY, { savedAt: this.nowMs, state } satisfies Cached);
  }

  tick(): void {
    this.nowMs = this.deps.now();
    if (this.status === 'live' && this.nowMs - this.receivedAt > STALE_AFTER_MS) this.status = 'stale';
    // A connection can die without an error (a cut tunnel): no message and no ping for three intervals ends it.
    if (this.source && this.nowMs - this.heardAt > WATCHDOG_PINGS * this.pingMs) this.lost();
  }
}

export function createLive(): Live {
  return new Live({
    eventSource: (url) => new EventSource(url) as unknown as EventSourceLike,
    storage: safeStorage(),
    now: () => Date.now(),
    probe: probeAuth,
  });
}

/** Asks the backend who the browser is. A 401, a redirect to the login page or a login page served as the answer mean
 * the Access session is over; a 403 means the address belongs to no team. */
export async function probeAuth(): Promise<AuthProbe> {
  try {
    const res = await fetch('/api/me', { redirect: 'manual', cache: 'no-store', headers: { Accept: 'application/json' } });
    if (res.type === 'opaqueredirect' || res.redirected || res.status === 401) return 'expired';
    if (res.status === 403) return 'denied';
    if (!res.ok) return 'down';
    return (res.headers.get('content-type') ?? '').includes('json') ? 'ok' : 'expired';
  } catch {
    return 'down';
  }
}
