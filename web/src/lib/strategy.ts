import { mean, rangeFor, type TyreRange } from './conditions';
import type { Fit, Params, Rival, State } from './types';

/** "Pascal Paczkowski" as "P. Paczkowski". */
export function shortName(driver: string): string {
  const parts = driver.trim().split(/\s+/);
  return parts.length < 2 ? driver : `${parts[0][0]}. ${parts.slice(1).join(' ')}`;
}

/** True for a car driven by the AI. */
export const isAI = (r: Pick<Rival, 'control'>): boolean => r.control === 1;

/** The forecast stop of a rival as laps, rounded; null when unknown. */
export const stopLaps = (r: Pick<Rival, 'stop_in'>): number | null => (r.stop_in == null ? null : Math.round(r.stop_in));

/** Parameters with the fitted values filled in; a value the fit could not tell stays as it is. */
export function applyFit(p: Params, f: Fit): Params {
  return {
    ...p,
    fuel_effect: f.fuel_effect != null && f.fuel_effect > 0 ? round(f.fuel_effect, 4) : p.fuel_effect,
    deg_per_lap: f.deg_per_lap != null && f.deg_per_lap > 0 ? round(f.deg_per_lap, 4) : p.deg_per_lap,
  };
}

/** True when applying the fit would change a parameter. */
export function fitDiffers(p: Params, f: Fit): boolean {
  const a = applyFit(p, f);
  return a.fuel_effect !== p.fuel_effect || a.deg_per_lap !== p.deg_per_lap;
}

const round = (v: number, digits: number) => Math.round(v * 10 ** digits) / 10 ** digits;

export type Level = 'info' | 'warn' | 'bad';

export interface Alert {
  id: string;
  level: Level;
  /** i18n key and its parameters. */
  key: string;
  params?: Record<string, string | number>;
}

const WHEELS = ['FL', 'FR', 'RL', 'RR'];
const WET_COMPOUND = /wet|rain|inter/i;
/** Rain or wetness above which slicks are the wrong tyre. */
const WET_TRACK = 0.3;
/** Wetness or rain from which a stop for wet tyres should be prepared, and below which a wet track is dry again. */
const WET_PREPARE = 0.15;
const WET_PREPARE_RAIN = 0.12;
const DRY_AGAIN = 0.1;
const RAIN_RISING = 0.05;
const MAX_ALERTS = 8;

/**
 * Things worth a look right now, derived from the state: rain and tyre choice, full course yellow, tyre
 * temperatures against the working ranges, rivals that stop soon and a better moment for our stop.
 */
export function alerts(
  s: State | null | undefined,
  ranges: TyreRange[],
  fallback: { low: number; high: number },
): Alert[] {
  if (!s) return [];
  const out: Alert[] = [];

  const w = s.weather;
  if (w) {
    const compound = `${s.tyres?.compound_front ?? ''} ${s.tyres?.compound_rear ?? ''}`.trim();
    const wetness = w.wetness ?? 0;
    const rain = w.rain ?? 0;
    const h = w.history.filter((p) => p.wetness != null);
    const rising = h.length >= 4 && (h[h.length - 1].wetness as number) - (h[h.length - 4].wetness as number) >= RAIN_RISING;
    if (rising) out.push({ id: 'rain-rising', level: 'warn', key: 'alert.rainRising' });
    if (compound) {
      const pct = Math.round(Math.max(wetness, rain) * 100);
      if (!WET_COMPOUND.test(compound)) {
        if (wetness >= WET_TRACK || rain >= WET_TRACK) out.push({ id: 'wet-now', level: 'bad', key: 'alert.wetNow', params: { pct } });
        else if (wetness >= WET_PREPARE || rain >= WET_PREPARE_RAIN || rising) out.push({ id: 'wet-prepare', level: 'warn', key: 'alert.wetPrepare', params: { pct } });
      } else if (wetness < DRY_AGAIN && rain < 0.05) {
        out.push({ id: 'slick-again', level: 'info', key: 'alert.slickAgain', params: { pct } });
      }
    }
  }

  if (s.fcy && s.params && s.params.pit_through_green > s.params.pit_through_fcy && s.params.pit_through_fcy > 0) {
    out.push({
      id: 'fcy',
      level: 'info',
      key: 'alert.fcy',
      params: { s: Math.round((s.params.pit_through_green - s.params.pit_through_fcy) * 10) / 10 },
    });
  }

  const car = s.car;
  if (car) {
    if ((car.dent_max ?? 0) >= 1) out.push({ id: 'damage', level: car.dent_max === 2 ? 'bad' : 'warn', key: car.dent_max === 2 ? 'alert.damageHeavy' : 'alert.damage' });
    if (car.detached) out.push({ id: 'detached', level: 'bad', key: 'alert.detached' });
    if (car.overheating) out.push({ id: 'overheating', level: 'bad', key: 'alert.overheating' });
    if ((car.penalties ?? 0) > 0) out.push({ id: 'penalties', level: 'warn', key: 'alert.penalties', params: { n: car.penalties as number } });
  }
  for (const a of s.agents ?? []) {
    if (!a.connected) out.push({ id: `agent-${a.agent_id}`, level: 'bad', key: 'alert.agentLost', params: { name: a.agent_id } });
  }

  const y = s.tyres;
  if (y?.surface) {
    for (const axle of [0, 1] as const) {
      const range = rangeFor(axle === 0 ? y.compound_front : y.compound_rear, ranges, fallback);
      for (const i of axle === 0 ? [0, 1] : [2, 3]) {
        const tread = y.surface[i];
        if (!tread) continue;
        const t = mean(tread);
        if (t < range.low - 10) out.push({ id: `cold-${i}`, level: 'info', key: 'alert.tyreCold', params: { w: WHEELS[i], t: Math.round(t), low: range.low } });
        else if (t > range.high + 10) out.push({ id: `hot-${i}`, level: 'warn', key: 'alert.tyreHot', params: { w: WHEELS[i], t: Math.round(t), high: range.high } });
      }
    }
  }

  const st = s.strategy;
  if (st) {
    for (const r of st.rivals) {
      const gap = r.gap;
      if (gap == null || Math.abs(gap) > 45 || r.in_pits || r.stop_basis === 'assumed') continue; // a guess from our consumption is not worth a note
      const laps = stopLaps(r);
      if (laps != null && laps <= 2) {
        out.push({
          id: `stop-${r.car_id}`,
          level: 'info',
          key: gap > 0 ? 'alert.rivalAheadStops' : 'alert.rivalBehindStops',
          params: { name: shortName(r.driver) || r.vehicle_name, n: laps },
        });
      }
    }
    const rj = st.rejoin;
    if (rj && rj.best_in > 0) {
      const now = rj.window[0]?.lost ?? rj.lost;
      const best = rj.window.find((p) => p.in_laps === rj.best_in)?.lost ?? now;
      if (best < now) out.push({ id: 'better-window', level: 'info', key: 'alert.betterWindow', params: { n: rj.best_in, now, best } });
    }
  }
  return out.slice(0, MAX_ALERTS);
}
