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
  params: Params;
  overrides: Record<string, Override>;
}

export interface PlanRow {
  driver: string;
  duration: number; // seconds
}

export type OverrideKey = 'fuel' | 'fuel_per_lap';
