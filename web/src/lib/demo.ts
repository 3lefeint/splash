import { normalizeState } from './normalize';
import type { PitSetup, State, TrackMap } from './types';

/**
 * Invented but realistic values, shown on the dashboard while its tiles are arranged and no agent sends data, so the
 * tiles have their real size and line breaks. Nothing here is stored or sent anywhere.
 */
export function demoState(): State {
  const et = 7200;
  const lap = 108.4;
  const far = '2100-01-01T00:00:00Z'; // the age of the data of an agent is never negative, so it reads "just now"
  const state: State = {
    agent_id: 0,
    agent_name: 'demo',
    ready: true,
    session_id: 'demo',
    track: 'Demo Track',
    et,
    time_left: 14_400,
    track_length: 5000,
    car: {
      car_id: 0,
      vehicle_name: 'Demo GT3 #12',
      driver: 'Driver A',
      class: 'GT3',
      place: 5,
      lap: 54,
      fuel: 62.4,
      fuel_capacity: 100,
      fuel_per_lap: 3.21,
      lap_time: lap,
      wear: [0.78, 0.77, 0.74, 0.73],
      fuel_overridden: false,
      fuel_per_lap_overridden: false,
      control: 0,
      pit_state: 0,
      penalties: 0,
      dent_max: 0,
    },
    race: { laps_to_stop: 19, laps_left: 133, stops_remaining: 6, save_target: 3.1, stint_end: et + 19 * lap },
    stint_plan: {
      entries: [
        { no: 1, driver: 'Driver A', start: 0, end: 3600, done: true, current: false, driver_swap: false },
        { no: 2, driver: 'Driver B', start: 3600, end: 7500, done: false, current: true, driver_swap: true },
        { no: 3, driver: 'Driver C', start: 7500, end: 11_400, done: false, current: false, driver_swap: true },
      ],
      shortfall: 0,
      current_deviation: 95,
      pit_loss: 62,
    },
    opponents: {
      ahead: { car_id: 7, vehicle_name: 'Demo GT3 #7', driver: 'Driver D', place: 4, gap: 3.4, trend: -0.21 },
      behind: { car_id: 21, vehicle_name: 'Demo GT3 #21', driver: 'Driver E', place: 6, gap: 1.9, trend: 0.12 },
    },
    field: [
      { car_id: 7, vehicle_name: 'Demo GT3 #7', driver: 'Driver D', class: 'GT3', place: 4, lap_dist: 1980, in_pits: false, ours: false },
      { car_id: 0, vehicle_name: 'Demo GT3 #12', driver: 'Driver A', class: 'GT3', place: 5, lap_dist: 1790, in_pits: false, ours: true },
      { car_id: 21, vehicle_name: 'Demo GT3 #21', driver: 'Driver E', class: 'GT3', place: 6, lap_dist: 1650, in_pits: false, ours: false },
      { car_id: 33, vehicle_name: 'Demo GT3 #33', driver: 'Driver F', class: 'GT3', place: 7, lap_dist: 3900, in_pits: false, ours: false },
      { car_id: 45, vehicle_name: 'Demo GT3 #45', driver: 'Driver G', class: 'GT3', place: 8, lap_dist: 4800, in_pits: true, ours: false },
    ],
    tyres: {
      surface: [[91, 95, 93], [92, 96, 94], [86, 90, 88], [87, 91, 89]],
      inner: [[82, 84, 83], [83, 85, 84], [79, 81, 80], [80, 82, 81]],
      carcass: [80, 81, 77, 78],
      pressure: [168, 169, 165, 166],
      brake: [520, 545, 430, 455],
      compound_front: 'Medium',
      compound_rear: 'Medium',
    },
    pit_menu: { category: 'FUEL:', choice: '+60', choice_index: 3, choices: 10 },
    strategy: {
      rivals: [
        { car_id: 7, vehicle_name: 'Demo GT3 #7', driver: 'Driver D', place: 4, in_pits: false, gap: 3.4, pace: 108.9, rate: -0.4, stops: 2, stint_laps: 12, fuel: 41.3, fuel_laps_left: 13, wear: [0.8, 0.8, 0.78, 0.78], stop_in: 11, stop_basis: 'fuel' },
        { car_id: 21, vehicle_name: 'Demo GT3 #21', driver: 'Driver E', place: 6, in_pits: false, gap: -1.9, pace: 109.6, rate: 0.3, stops: 2, stint_laps: 16, fuel: 22.5, fuel_laps_left: 7, wear: [0.7, 0.7, 0.68, 0.68], stop_in: 6, stop_basis: 'fuel' },
      ],
      rejoin: { lap_dist: 1500, loss: 62, position: 7, lost: 2, passed_by: ['Driver F', 'Driver G'], also_stop: [], window: [], best_in: 3, incomplete: false },
    },
    agents: [{ agent_id: 'demo', token_id: 0, version: '0.2.0', connected: true, since: far, last_data: far }],
    fit: { laps: 18, fuel_effect: 0.031, deg_per_lap: 0.045, base: 107.1, r2: 0.82, min_laps: 5, excluded: [] },
    weather: {
      ambient: 21,
      track: 33,
      rain: 0,
      wetness: 0,
      dark_cloud: 0.2,
      history: [0, 1, 2, 3, 4, 5].map((i) => ({ et: et - 1800 + i * 360, ambient: 20 + i * 0.2, track: 31 + i * 0.4, rain: 0, wetness: 0 })),
    },
    pits: {
      rows: [
        { no: 1, entry: 3540, lane: 58, stopped: 31, work: 28, fuel_time: 24, through: 27, race_part: 22, net: 5, fuel: 62, tyres: true, swap: true },
      ],
      suggest: { pit_through: 5, refuel_rate: 2.6, driver_swap: null },
    },
    params: {
      track: 'Demo Track', car: 'Demo GT3 #12', pit_through_green: 5, pit_through_fcy: 3, refuel_rate: 2.6, tyre_change: 17,
      driver_swap: 12, fuel_effect: 0.03, deg_per_lap: 0.04, save_cost: 0.3, reserve: 3,
    },
    overrides: {},
  };
  return normalizeState(state);
}

/** An invented outline of 5 km for the map tile. */
export function demoMap(): TrackMap {
  const n = 160;
  const points: [number, number][] = [];
  for (let i = 0; i < n; i++) {
    const a = (i / n) * Math.PI * 2;
    const r = 1 + 0.18 * Math.sin(3 * a) + 0.08 * Math.cos(5 * a);
    points.push([Math.round(Math.cos(a) * 520 * r), Math.round(Math.sin(a) * 330 * r)]);
  }
  return { track: 'Demo Track', length: 5000, points };
}

/** Pit set-ups shown in the set-ups tile of the arranged dashboard. */
export const DEMO_SETUPS: PitSetup[] = [
  { car: 'Demo GT3 #12', name: 'Fuel only', steps: [{ category: 'FUEL:', choice: '+60' }] },
  { car: 'Demo GT3 #12', name: 'Fuel and tyres', steps: [{ category: 'FUEL:', choice: '+60' }, { category: 'TIRES:', choice: 'Medium' }] },
  { car: 'Demo GT3 #12', name: 'Driver change', steps: [{ category: 'DRIVER:', choice: 'Driver B' }] },
  { car: 'Demo GT3 #12', name: 'Splash', steps: [{ category: 'FUEL:', choice: '+10' }] },
];
