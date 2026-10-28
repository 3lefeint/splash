import { describe, expect, it } from 'vitest';
import { DEFAULT_RANGES, FALLBACK_RANGE } from './conditions';
import { alerts, applyFit, fitDiffers, shortName, stopLaps } from './strategy';
import type { Params, State } from './types';

const params = { pit_through_green: 20, pit_through_fcy: 8, fuel_effect: 0, deg_per_lap: 0 } as Params;
const run = (s: Partial<State>) => alerts(s as State, DEFAULT_RANGES, FALLBACK_RANGE);
const ids = (s: Partial<State>) => run(s).map((a) => a.id);

describe('shortName', () => {
  it('abbreviates the first name', () => {
    expect(shortName('Pascal Paczkowski')).toBe('P. Paczkowski');
    expect(shortName('Max')).toBe('Max');
  });
});

describe('fit', () => {
  const fit = { laps: 20, fuel_effect: 0.031234, deg_per_lap: 0.05, base: 90, r2: 0.9 };
  it('fills only usable values, rounded', () => {
    expect(applyFit(params, fit)).toMatchObject({ fuel_effect: 0.0312, deg_per_lap: 0.05 });
    expect(applyFit(params, { ...fit, fuel_effect: -0.01, deg_per_lap: null })).toMatchObject({ fuel_effect: 0, deg_per_lap: 0 });
    expect(fitDiffers(params, fit)).toBe(true);
    expect(fitDiffers(applyFit(params, fit), fit)).toBe(false);
  });
});

describe('alerts', () => {
  it('warns about slicks on a wet track and rising rain', () => {
    const history = [0, 0.02, 0.05, 0.12].map((wetness, i) => ({ et: i, ambient: 20, track: 25, rain: 0, wetness }));
    const s = { weather: { ambient: 20, track: 25, rain: 0.4, wetness: 0.4, dark_cloud: 0, history }, tyres: { compound_front: 'Medium', compound_rear: 'Medium' } };
    expect(ids(s)).toEqual(expect.arrayContaining(['wet-now', 'rain-rising']));
    expect(ids({ ...s, tyres: { compound_front: 'Wet', compound_rear: 'Wet' } })).not.toContain('wet-now');
  });

  it('tells when to prepare for wet tyres, to switch, and when slicks work again', () => {
    const base = { ambient: 20, track: 25, dark_cloud: 0, history: [] };
    const tyres = (c: string) => ({ compound_front: c, compound_rear: c });
    expect(ids({ weather: { ...base, rain: 0, wetness: 0.18 }, tyres: tyres('Medium') })).toEqual(['wet-prepare']);
    expect(ids({ weather: { ...base, rain: 0.5, wetness: 0.1 }, tyres: tyres('Medium') })).toEqual(['wet-now']);
    expect(ids({ weather: { ...base, rain: 0, wetness: 0.05 }, tyres: tyres('Wet') })).toEqual(['slick-again']);
    expect(ids({ weather: { ...base, rain: 0, wetness: 0.4 }, tyres: tyres('Wet') })).toEqual([]);
    expect(ids({ weather: { ...base, rain: 0, wetness: 0.4 } })).toEqual([]); // compound unknown: no advice
  });

  it('reports damage, penalties and agents that are gone', () => {
    const car = { dent_max: 2, penalties: 1, overheating: true } as State['car'];
    const agents = [
      { agent_id: 'driver-pc', role: 'driver' as const, connected: true, since: '', last_data: '' },
      { agent_id: 'spotter-pc', role: 'spotter' as const, connected: false, since: '', last_data: '' },
    ];
    expect(ids({ car, agents })).toEqual(['damage', 'overheating', 'penalties', 'agent-spotter-pc']);
    expect(run({ car: { dent_max: 1 } as State['car'] })[0]).toMatchObject({ level: 'warn', key: 'alert.damage' });
  });

  it('points out the cheaper stop under full course yellow', () => {
    const a = run({ fcy: true, params });
    expect(a[0]).toMatchObject({ id: 'fcy', params: { s: 12 } });
    expect(ids({ fcy: false, params })).toEqual([]);
  });

  it('compares tyre temperatures with the range of the compound', () => {
    const tyres = { surface: [[60, 62, 61], [90, 90, 90], [90, 90, 90], [120, 125, 122]], compound_front: 'Medium', compound_rear: 'Medium' };
    expect(ids({ tyres })).toEqual(['cold-0', 'hot-3']);
    const soft = { ...tyres, compound_front: 'Soft', compound_rear: 'Soft' };
    expect(ids({ tyres: soft })).toEqual(['cold-0', 'hot-3']); // soft is 85-105, so 90 is fine
  });

  it('reports rivals that stop soon and a better moment to stop', () => {
    const rival = (car_id: number, gap: number, stop_in: number | null) => ({
      car_id, vehicle_name: 'v', driver: 'A B', place: 1, in_pits: false, gap, pace: null, rate: null, stops: 0, stint_laps: 5,
      fuel: null, fuel_laps_left: null, wear: null, stop_in, stop_basis: 'fuel' as const,
    });
    const strategy = {
      rivals: [rival(1, 10, 1.2), rival(2, -60, 1), rival(3, -5, 9), rival(4, -8, null), { ...rival(5, 3, 1), stop_basis: 'assumed' as const }],
      rejoin: { loss: 40, position: 5, lost: 2, passed_by: null, also_stop: null, incomplete: false, best_in: 3, window: [{ in_laps: 0, lost: 2 }, { in_laps: 3, lost: 1 }] },
    };
    expect(run({ strategy })).toEqual([
      { id: 'stop-1', level: 'info', key: 'alert.rivalAheadStops', params: { name: 'A. B', n: 1 } },
      { id: 'better-window', level: 'info', key: 'alert.betterWindow', params: { n: 3, now: 2, best: 1 } },
    ]);
  });

  it('has nothing to say without a state', () => {
    expect(alerts(null, DEFAULT_RANGES, FALLBACK_RANGE)).toEqual([]);
    expect(stopLaps({ stop_in: null })).toBeNull();
    expect(stopLaps({ stop_in: 2.6 })).toBe(3);
  });
});
