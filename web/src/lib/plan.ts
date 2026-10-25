import { clockAt, duration } from './format';
import type { PlanEntry, PlanRow, State } from './types';

/** Seconds from `now` until the given game time, never negative. */
export function until(et: number, nowEt: number): number {
  return Math.max(et - nowEt, 0);
}

export function currentEntry(state: State): PlanEntry | undefined {
  return state.stint_plan?.entries.find((e) => e.current);
}

/** The first stint after the current one, which is the one the next swap leads into. */
export function nextEntry(state: State): PlanEntry | undefined {
  const entries = state.stint_plan?.entries ?? [];
  const i = entries.findIndex((e) => e.current);
  return i >= 0 ? entries[i + 1] : entries.find((e) => !e.done);
}

/** The next stint of `driver` that has not ended yet, current or future. */
export function nextStintOf(state: State, driver: string): PlanEntry | undefined {
  return state.stint_plan?.entries.find((e) => !e.done && !e.reserve && e.driver === driver);
}

export function drivers(rows: PlanRow[]): string[] {
  return [...new Set(rows.map((r) => r.driver).filter(Boolean))];
}

/** Builds a plan of `count` equal stints with the drivers rotating, in seconds. */
export function generate(count: number, minutes: number, names: string[]): PlanRow[] {
  if (count < 1 || minutes <= 0 || names.length === 0) return [];
  return Array.from({ length: count }, (_, i) => ({ driver: names[i % names.length], duration: minutes * 60 }));
}

function csvCell(v: string): string {
  return /[;"\n]/.test(v) ? `"${v.replaceAll('"', '""')}"` : v;
}

/**
 * The recomputed plan as CSV for printing: stint, driver, wall clock start and end, race time start and
 * duration, and the status. Semicolons separate the columns so it opens directly in spreadsheet tools of
 * German locale.
 */
export function toCsv(state: State, nowMs: number, labels: { done: string; current: string; planned: string; reserve: string }): string {
  const rows = [['No', 'Driver', 'Start', 'End', 'Race time start', 'Duration', 'Status']];
  for (const e of state.stint_plan?.entries ?? []) {
    rows.push([
      String(e.no),
      e.driver,
      clockAt(e.start, state.et, nowMs),
      clockAt(e.end, state.et, nowMs),
      duration(e.start),
      duration(e.end - e.start),
      e.done ? labels.done : e.current ? labels.current : e.reserve ? labels.reserve : labels.planned,
    ]);
  }
  return rows.map((r) => r.map(csvCell).join(';')).join('\r\n') + '\r\n';
}
