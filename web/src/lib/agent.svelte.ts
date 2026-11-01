import { getAgents, useAgent } from './api';
import { readJson, safeStorage, writeJson, type KeyValueStore } from './storage';
import { live } from './store';
import type { AgentInfo } from './types';

const KEY = 'splash.agent';
const REFRESH_MS = 15_000;

export interface AgentsDeps {
  storage: KeyValueStore | null;
  fetchAgents: () => Promise<AgentInfo[]>;
  /** Tells the API client and the live stream which agent to use. */
  apply: (id: number | null) => void;
}

/** The agents of the team and the one the app looks at. null means the member's own agent. */
export class Agents {
  list = $state<AgentInfo[]>([]);
  selected = $state<number | null>(null);
  private timer: ReturnType<typeof setInterval> | null = null;

  constructor(private deps: AgentsDeps) {
    const stored = readJson<number | null>(deps.storage, KEY);
    this.selected = typeof stored === 'number' && Number.isInteger(stored) && stored >= 0 ? stored : null;
  }

  /** Applies the stored choice and keeps the list fresh. Call before the live stream is started. */
  start(): void {
    this.deps.apply(this.selected);
    void this.refresh();
    if (!this.timer) this.timer = setInterval(() => void this.refresh(), REFRESH_MS);
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  select(id: number | null): void {
    this.selected = id;
    writeJson(this.deps.storage, KEY, id);
    this.deps.apply(id);
  }

  async refresh(): Promise<void> {
    try {
      this.list = await this.deps.fetchAgents();
    } catch {
      return; // the rest of the app shows connection problems
    }
    // A choice that no longer exists (revoked, other team) would only give 404s: go back to the own agent.
    if (this.selected !== null && !this.list.some((a) => a.id === this.selected)) this.select(null);
  }
}

export const agents = new Agents({
  storage: safeStorage(),
  fetchAgents: getAgents,
  apply: (id) => {
    useAgent(id);
    live.setAgent(id);
  },
});
