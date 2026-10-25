import { describe, expect, it } from 'vitest';
import { applySuggestion, differs } from './pits';
import type { Params, PitSuggest } from './types';

const params: Params = {
  track: 't', car: 'c', pit_through_green: 30, pit_through_fcy: 20, refuel_rate: 2, tyre_change: 25, driver_swap: 15,
  parallel_service: false, fuel_effect: 0, deg_per_lap: 0, save_cost: 0, reserve: 3,
};
const none: PitSuggest = { pit_through: null, refuel_rate: null, tyre_change: null, driver_swap: null, parallel_service: null };

describe('applySuggestion', () => {
  it('replaces only the suggested values', () => {
    const p = applySuggestion(params, { ...none, pit_through: 41.5, parallel_service: true });
    expect(p.pit_through_green).toBe(41.5);
    expect(p.parallel_service).toBe(true);
    expect(p.refuel_rate).toBe(2);
    expect(p.pit_through_fcy).toBe(20);
  });
  it('knows whether anything would change', () => {
    expect(differs(params, none)).toBe(false);
    expect(differs(params, { ...none, refuel_rate: 2 })).toBe(false);
    expect(differs(params, { ...none, refuel_rate: 2.5 })).toBe(true);
  });
});
