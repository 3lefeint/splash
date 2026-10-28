// Shapes of the backend's /api/state JSON (see backend/internal/engine).

export interface Override {
  key: string;
  value: number;
  set_at: string;
}

export interface Car {
  car_id: number;
  vehicle_name: string;
  driver: string;
  class: string;
  place: number;
  lap: number;
  fuel: number;
  fuel_capacity: number;
  fuel_per_lap: number | null;
  lap_time: number | null;
  wear: number[];
  fuel_overridden: boolean;
  fuel_per_lap_overridden: boolean;
  control?: number; // 0 local player, 1 local AI, 2 remote
  pit_state?: number; // 0 none, 1 requested, 2 entering, 3 stopped, 4 exiting
  penalties?: number;
  dent_max?: number; // 0 none, 1 some, 2 heavy body damage
  overheating?: boolean;
  detached?: boolean;
}

export interface Race {
  laps_to_stop: number;
  laps_left: number;
  stops_remaining: number;
  save_target: number | null;
  stint_end: number; // game time at which the fuel is down to the reserve
}

export interface PlanEntry {
  no: number;
  driver: string;
  start: number; // game time
  end: number;
  done: boolean;
  current: boolean;
  driver_swap: boolean;
  reserve?: boolean; // not needed to reach the finish with the current times
}

export interface Plan {
  entries: PlanEntry[];
  shortfall: number;
  current_deviation: number;
  pit_loss: number;
}

export interface Opponent {
  car_id: number;
  vehicle_name: string;
  driver: string;
  place: number;
  gap: number | null;
  trend: number | null; // seconds per lap, negative = gap shrinking
}

export interface Params {
  track: string;
  car: string;
  pit_through_green: number;
  pit_through_fcy: number;
  refuel_rate: number;
  tyre_change: number;
  driver_swap: number;
  parallel_service: boolean;
  fuel_effect: number;
  deg_per_lap: number;
  save_cost: number;
  reserve: number;
}

export interface FieldCar {
  car_id: number;
  vehicle_name: string;
  driver: string;
  class: string;
  place: number;
  lap_dist: number; // metres along the lap
  in_pits: boolean;
  ours: boolean;
  off_track?: boolean;
}

export interface TrackMap {
  track: string;
  length: number;
  points: [number, number][]; // world x, z in metres
}

/**
 * A named stretch of the track. from and to are metres along the lap in driving direction (to below from: the stretch
 * crosses the start line), null while not marked. label is the place of the name on the map in world coordinates.
 */
export interface Corner {
  name: string;
  from: number | null;
  to: number | null;
  label: [number, number] | null;
}

/** A track that has a stored outline. */
export interface TrackInfo {
  track: string;
  length: number;
}

export interface MapProgress {
  track: string;
  covered: number; // 0 to 1
  points: [number, number][];
}

export interface WearStats {
  segments: number;
  laps: number;
  previous_set?: boolean; // the figures are those of the set before the current one
  recent: number[][]; // [wheel][segment] percentage points lost per lap
  base?: number[][];
  per_lap: { lap: number; loss: number }[];
  per_wheel: number[];
  trend_pct: number | null; // positive: the tyres wear faster than at the start of the set
}

export interface Rival {
  car_id: number;
  vehicle_name: string;
  driver: string;
  place: number;
  control?: number;
  in_pits: boolean;
  gap: number | null; // seconds the rival is ahead of us, negative when behind
  pace: number | null;
  rate: number | null; // seconds per lap that we gain on it, positive when it is slower
  stops: number;
  stint_laps: number;
  fuel: number | null;
  fuel_laps_left: number | null;
  wear: number[] | null; // each tyre, 1 is new
  stop_in: number | null; // forecast laps until its stop
  stop_basis?: 'fuel' | 'assumed' | 'stint';
}

export interface Rejoin {
  loss: number;
  position: number;
  lost: number;
  passed_by: string[] | null;
  also_stop: string[] | null;
  window: { in_laps: number; lost: number }[];
  best_in: number;
  incomplete: boolean;
}

export interface VsCar {
  name: string;
  delta: (number | null)[]; // seconds per section, ours minus theirs: negative where we are faster
  laps: number;
}

export interface PitLaneState {
  entry?: [number, number];
  exit?: [number, number];
  boxes: [number, number][];
  our_box?: [number, number];
}

/** How our car spends a lap along the track, section by section. */
export interface Sections {
  segments: number;
  laps: number;
  ahead?: VsCar;
  behind?: VsCar;
  best?: (number | null)[];
  fuel?: (number | null)[]; // litres per section
  speed?: (number | null)[]; // m/s
  brake?: (number | null)[]; // 0..1
  pits?: PitLaneState;
}

export interface AgentStatus {
  agent_id: string;
  role: 'driver' | 'spotter';
  version?: string;
  session_id?: string;
  connected: boolean;
  since: string; // connected since, or gone since
  last_data: string;
}

export interface Strategy {
  rivals: Rival[];
  rejoin?: Rejoin;
}

export interface Fit {
  laps: number;
  fuel_effect: number | null;
  deg_per_lap: number | null;
  base: number;
  r2: number;
  reason?: 'few_laps' | 'collinear';
}

export interface Step {
  category: string;
  choice: string;
}

export interface PitSetup {
  car: string;
  name: string;
  steps: Step[];
}

export interface CommandStatus {
  id: string;
  name: string;
  state: 'pending' | 'done' | 'failed';
  message?: string;
  applied?: Step[];
  at: string;
}

export interface PitMenu {
  category: string;
  choice: string;
  choice_index: number;
  choices: number;
}

export interface Tyres {
  surface?: number[][]; // [wheel][left, centre, right] in °C
  inner?: number[][];
  carcass?: number[];
  pressure?: number[]; // kPa
  brake?: number[];
  water?: number;
  oil?: number;
  compound_front?: string;
  compound_rear?: string;
}

export interface WeatherPoint {
  et: number;
  ambient: number | null;
  track: number | null;
  rain: number | null;
  wetness: number | null;
}

export interface Weather {
  ambient: number | null;
  track: number | null;
  rain: number | null; // 0..1
  wetness: number | null; // 0..1
  dark_cloud: number | null;
  history: WeatherPoint[];
}

export interface StopRow {
  no: number;
  entry: number;
  lane: number | null; // pit entry to exit
  stopped: number | null; // standing in the box
  work: number | null; // until the last fuel or tyre change was done
  fuel_time: number | null;
  through: number | null;
  fuel: number | null;
  tyres: boolean;
  swap: boolean;
}

export interface PitSuggest {
  pit_through: number | null;
  refuel_rate: number | null;
  tyre_change: number | null;
  driver_swap: number | null;
  parallel_service: boolean | null;
}

export interface PitStats {
  rows: StopRow[];
  suggest?: PitSuggest;
}

export interface State {
  ready: boolean;
  session_id?: string;
  track?: string;
  et: number;
  time_left: number;
  car?: Car;
  race?: Race;
  stint_plan?: Plan;
  opponents: { ahead: Opponent | null; behind: Opponent | null };
  track_length?: number;
  field?: FieldCar[];
  wear_stats?: WearStats;
  tyres?: Tyres;
  pit_menu?: PitMenu;
  command?: CommandStatus;
  strategy?: Strategy;
  agents?: AgentStatus[];
  sections?: Sections;
  fit?: Fit;
  fcy?: boolean;
  weather?: Weather;
  pits?: PitStats;
  params: Params;
  overrides: Record<string, Override>;
}

export interface PlanRow {
  driver: string;
  duration: number; // seconds
}

export type OverrideKey = 'fuel' | 'fuel_per_lap';

export interface SessionSummary {
  session_id: string;
  track: string;
  session_type: string;
  car: string;
  started_at: string;
  last_seen: string;
  last_et: number;
  laps: number;
  stops: number;
  conditions: number;
  ai_laps: number;
  player_laps: number;
}
