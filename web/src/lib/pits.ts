import type { Params, PitSuggest } from './types';

/** The parameters with every value the stops suggest filled in; values without a suggestion stay as they are. */
export function applySuggestion(p: Params, s: PitSuggest): Params {
  return {
    ...p,
    pit_through_green: s.pit_through ?? p.pit_through_green,
    refuel_rate: s.refuel_rate ?? p.refuel_rate,
    tyre_change: s.tyre_change ?? p.tyre_change,
    driver_swap: s.driver_swap ?? p.driver_swap,
    parallel_service: s.parallel_service ?? p.parallel_service,
  };
}

/** True when applying the suggestion would change something. */
export function differs(p: Params, s: PitSuggest): boolean {
  const a = applySuggestion(p, s);
  return (
    a.pit_through_green !== p.pit_through_green ||
    a.refuel_rate !== p.refuel_rate ||
    a.tyre_change !== p.tyre_change ||
    a.driver_swap !== p.driver_swap ||
    a.parallel_service !== p.parallel_service
  );
}
