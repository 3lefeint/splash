import type { WearStats } from './types';

/** What is painted on the track: tyre wear, its change, time against another car or our best lap, fuel, speed or braking. */
export type Overlay = 'off' | 'wear' | 'delta' | 'time' | 'fuel' | 'speed' | 'brake';
export type Wheel = 'all' | '0' | '1' | '2' | '3';

/** Loss per lap by track section for one wheel or the mean of all four, in percentage points. */
export function sections(rows: number[][], wheel: Wheel): number[] {
  if (rows.length === 0) return [];
  const pick = wheel === 'all' ? rows : [rows[Number(wheel)]];
  return rows[0].map((_, s) => pick.reduce((sum, r) => sum + r[s], 0) / pick.length);
}

export interface Heat {
  values: number[];
  /** The value that gets the strongest colour; for the change view the colour scale is symmetric around 0. */
  max: number;
}

/** What to paint on the track. The change view needs the figures of the start of the tyre set. */
export function heat(stats: WearStats | undefined, overlay: Overlay, wheel: Wheel): Heat | null {
  if (!stats || (overlay !== 'wear' && overlay !== 'delta')) return null;
  const recent = sections(stats.recent, wheel);
  if (overlay === 'wear') return { values: recent, max: Math.max(...recent, 1e-9) };
  if (!stats.base) return null;
  const base = sections(stats.base, wheel);
  const values = recent.map((v, i) => v - base[i]);
  return { values, max: Math.max(...values.map(Math.abs), 1e-9) };
}

type Rgb = [number, number, number];
const COLD: Rgb = [58, 80, 104];
const WARM: Rgb = [255, 180, 84];
const HOT: Rgb = [255, 92, 108];
const GOOD: Rgb = [61, 220, 132];

function mix(a: Rgb, b: Rgb, t: number): string {
  const c = a.map((v, i) => Math.round(v + (b[i] - v) * t));
  return `rgb(${c[0]} ${c[1]} ${c[2]})`;
}

/** Colour of a value in the wear view: from calm to hot. */
export function heatColor(value: number, max: number): string {
  const t = Math.min(Math.max(value / max, 0), 1);
  return t < 0.5 ? mix(COLD, WARM, t * 2) : mix(WARM, HOT, (t - 0.5) * 2);
}

/** Colour of a value in the change view: green where the wear got smaller, red where it got larger. */
export function deltaColor(value: number, max: number): string {
  const t = Math.min(Math.abs(value) / max, 1);
  return mix(COLD, value >= 0 ? HOT : GOOD, t);
}

/** Index of the section with the highest value, or -1. */
export function hottest(values: number[]): number {
  let best = -1;
  values.forEach((v, i) => {
    if (best < 0 || v > values[best]) best = i;
  });
  return best;
}

/** Colour of a tyre by what is left of it (1 is new): green when new, orange around half, red when worn out. */
export function wearColor(left: number): string {
  const r = Math.min(Math.max(left, 0), 1);
  const hue = r >= 0.5 ? 30 + ((r - 0.5) / 0.5) * 100 : (r / 0.5) * 30;
  return `hsl(${Math.round(hue)} 75% 45%)`;
}
