import { describe, expect, it } from 'vitest';
import { currentEntry, drivers, generate, nextEntry, nextStintOf, toCsv, until } from './plan';
import type { PlanEntry, State } from './types';

const entry = (no: number, driver: string, start: number, end: number, flags: Partial<PlanEntry> = {}): PlanEntry => ({
  no, driver, start, end, done: false, current: false, driver_swap: false, ...flags,
});

function state(entries: PlanEntry[]): State {
  return {
    ready: true, et: 5000, time_left: 1000, opponents: { ahead: null, behind: null },
    params: {} as State['params'], overrides: {},
    stint_plan: { entries, shortfall: 0, current_deviation: 0, pit_loss: 0 },
  };
}

const s = state([
  entry(1, 'a', 0, 3000, { done: true }),
  entry(2, 'b', 3100, 6000, { current: true }),
  entry(3, 'a', 6100, 9000),
  entry(4, 'b', 9100, 12000),
]);

describe('plan helpers', () => {
  it('finds current, next and the next stint of a driver', () => {
    expect(currentEntry(s)?.no).toBe(2);
    expect(nextEntry(s)?.no).toBe(3);
    expect(nextStintOf(s, 'a')?.no).toBe(3);
    expect(nextStintOf(s, 'b')?.no).toBe(2); // the running stint counts
    expect(nextStintOf(s, 'x')).toBeUndefined();
  });
  it('the next entry before any stint runs is the first open one', () => {
    expect(nextEntry(state([entry(1, 'a', 0, 10)]))?.no).toBe(1);
  });
  it('until never goes negative', () => {
    expect(until(100, 40)).toBe(60);
    expect(until(100, 140)).toBe(0);
  });
  it('lists drivers once, in order', () => {
    expect(drivers([{ driver: 'a', duration: 1 }, { driver: 'b', duration: 1 }, { driver: 'a', duration: 1 }, { driver: '', duration: 1 }])).toEqual(['a', 'b']);
  });
});

describe('generate', () => {
  it('rotates drivers', () => {
    expect(generate(4, 65, ['a', 'b', 'c']).map((r) => r.driver)).toEqual(['a', 'b', 'c', 'a']);
    expect(generate(1, 65, ['a'])[0].duration).toBe(3900);
  });
  it('returns nothing for unusable input', () => {
    expect(generate(0, 65, ['a'])).toEqual([]);
    expect(generate(3, 0, ['a'])).toEqual([]);
    expect(generate(3, 65, [])).toEqual([]);
  });
});

describe('toCsv', () => {
  it('writes a header, one row per stint and escapes cells', () => {
    const csv = toCsv(
      state([entry(1, 'a;b', 0, 3600, { done: true }), entry(2, 'c"d', 3700, 7300, { current: true }), entry(3, 'e', 7400, 11000, { reserve: true })]),
      new Date(2026, 0, 1, 12, 0, 0).getTime(),
      { done: 'done', current: 'running', planned: 'planned', reserve: 'reserve' },
    );
    const lines = csv.trimEnd().split('\r\n');
    expect(lines[0]).toBe('No;Driver;Start;End;Race time start;Duration;Status');
    expect(lines).toHaveLength(4);
    expect(lines[3].endsWith(';reserve')).toBe(true);
    expect(lines[1].startsWith('1;"a;b";')).toBe(true);
    expect(lines[1].endsWith(';done')).toBe(true);
    expect(lines[2]).toContain('"c""d"');
    expect(lines[2].endsWith(';running')).toBe(true);
  });
});
