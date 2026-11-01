import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Agents } from './agent.svelte';
import { Live, type EventSourceLike } from './live.svelte';
import type { KeyValueStore } from './storage';
import type { AgentInfo } from './types';

class Source implements EventSourceLike {
  onerror: ((e: unknown) => void) | null = null;
  onopen: ((e: unknown) => void) | null = null;
  closed = false;
  constructor(readonly url: string) {}
  addEventListener() {}
  close() {
    this.closed = true;
  }
}

function mem(initial: Record<string, string> = {}): KeyValueStore & { data: Record<string, string> } {
  const data = { ...initial };
  return { data, getItem: (k) => data[k] ?? null, setItem: (k, v) => void (data[k] = v) };
}

const info = (id: number, mine = false): AgentInfo => ({ id, name: `agent ${id}`, mine, active: true, last_sample: null, last_seen: null });

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('Live.setAgent', () => {
  function make() {
    const sources: Source[] = [];
    const live = new Live({ eventSource: (url) => { const s = new Source(url); sources.push(s); return s; }, storage: null, now: () => 0 });
    return { live, sources };
  }

  it('opens the stream of the member\'s own agent by default and the chosen one on request', () => {
    const { live, sources } = make();
    live.start();
    expect(sources.map((s) => s.url)).toEqual(['/api/stream']);
    live.setAgent(4);
    expect(sources.map((s) => s.url)).toEqual(['/api/stream', '/api/stream?agent=4']);
    expect(sources[0].closed).toBe(true);
    live.setAgent(null);
    expect(sources.at(-1)?.url).toBe('/api/stream');
    live.stop();
  });

  it('does nothing for the agent it already shows and waits for start when it is not running', () => {
    const { live, sources } = make();
    live.setAgent(4);
    expect(sources).toHaveLength(0);
    live.start();
    expect(sources.map((s) => s.url)).toEqual(['/api/stream?agent=4']);
    live.setAgent(4);
    expect(sources).toHaveLength(1);
    live.stop();
  });
});

describe('Agents', () => {
  it('applies a stored choice at start and ignores garbage', () => {
    const apply = vi.fn();
    new Agents({ storage: mem({ 'splash.agent': '3' }), fetchAgents: async () => [], apply }).start();
    expect(apply).toHaveBeenCalledWith(3);
    const apply2 = vi.fn();
    new Agents({ storage: mem({ 'splash.agent': '"x"' }), fetchAgents: async () => [], apply: apply2 }).start();
    expect(apply2).toHaveBeenCalledWith(null);
  });

  it('stores the choice and passes it on', () => {
    const storage = mem();
    const apply = vi.fn();
    const a = new Agents({ storage, fetchAgents: async () => [], apply });
    a.select(5);
    expect(a.selected).toBe(5);
    expect(storage.data['splash.agent']).toBe('5');
    expect(apply).toHaveBeenLastCalledWith(5);
    a.select(null);
    expect(storage.data['splash.agent']).toBe('null');
  });

  it('goes back to the own agent when the chosen one is no longer in the list', async () => {
    const apply = vi.fn();
    const a = new Agents({ storage: mem({ 'splash.agent': '9' }), fetchAgents: async () => [info(1, true), info(2)], apply });
    await a.refresh();
    expect(a.list.map((x) => x.id)).toEqual([1, 2]);
    expect(a.selected).toBeNull();
    expect(apply).toHaveBeenLastCalledWith(null);
  });

  it('keeps the choice when the list cannot be fetched', async () => {
    const a = new Agents({ storage: mem({ 'splash.agent': '2' }), fetchAgents: async () => { throw new Error('offline'); }, apply: vi.fn() });
    await a.refresh();
    expect(a.selected).toBe(2);
  });
});
