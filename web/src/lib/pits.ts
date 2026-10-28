import type { Params, PitSuggest } from './types';

/** The parameters with every value the stops suggest filled in; values without a suggestion stay as they are. */
export function applySuggestion(p: Params, s: PitSuggest): Params {
  return {
    ...p,
    pit_through_green: s.pit_through ?? p.pit_through_green,
    refuel_rate: s.refuel_rate ?? p.refuel_rate,
    driver_swap: s.driver_swap ?? p.driver_swap,
  };
}

/** True when applying the suggestion would change something. */
export function differs(p: Params, s: PitSuggest): boolean {
  const a = applySuggestion(p, s);
  return (
    a.pit_through_green !== p.pit_through_green ||
    a.refuel_rate !== p.refuel_rate ||
    a.driver_swap !== p.driver_swap
  );
}
