import { describe, expect, it } from 'vitest';
import { announcements } from './announce';
import type { Alert } from './strategy';
import type { State } from './types';

const entry = (no: number, driver: string, end: number, extra = {}) => ({ no, driver, start: end - 3600, end, done: false, current: false, driver_swap: true, ...extra });
const plan = (cur: number, next = 'B', nextExtra = {}) => ({
  entries: [entry(1, 'A', cur, { current: true }), entry(2, next, cur + 3600, nextExtra)],
  shortfall: 0,
  current_deviation: 0,
  pit_loss: 0,
});
const run = (s: Partial<State>, et = 0, alerts: Alert[] = []) => announcements(s as State, et, alerts).map((a) => a.id);

describe('announcements', () => {
  it('announces the stop at two laps, one lap and zero laps', () => {
    const race = (laps_to_stop: number, stops_remaining = 3) => ({ laps_to_stop, laps_left: 50, stops_remaining, save_target: null, stint_end: 0 });
    expect(run({ race: race(5) })).toEqual([]);
    expect(run({ race: race(2) })).toEqual(['stop-2']);
    expect(run({ race: race(0) })).toEqual(['stop-0']);
    expect(run({ race: race(1, 0) })).toEqual([]); // no stop is left to make
  });

  it('warns five minutes and one minute before a driver change, once at a time', () => {
    const s = (left: number) => ({ stint_plan: plan(left) });
    expect(run(s(900))).toEqual([]);
    expect(run(s(280))).toEqual(['swap-300']);
    expect(run(s(50))).toEqual(['swap-60']);
    expect(announcements(s(280) as State, 0, [])[0].params).toEqual({ min: 5, name: 'B' });
  });

  it('stays quiet when the same driver continues or only a reserve stint follows', () => {
    expect(run({ stint_plan: plan(100, 'A') })).toEqual([]);
    expect(run({ stint_plan: plan(100, 'B', { reserve: true }) })).toEqual([]);
  });

  it('passes on the alerts that deserve a sound and leaves the rest', () => {
    const alerts: Alert[] = [
      { id: 'fcy', level: 'info', key: 'alert.fcy' },
      { id: 'cold-0', level: 'info', key: 'alert.tyreCold' },
      { id: 'agent-x', level: 'bad', key: 'alert.agentLost' },
      { id: 'stop-3', level: 'info', key: 'alert.rivalAheadStops' },
    ];
    expect(run({}, 0, alerts)).toEqual(['fcy', 'agent-x']);
  });

  it('has nothing without a state', () => {
    expect(announcements(null, 0, [])).toEqual([]);
  });
});
