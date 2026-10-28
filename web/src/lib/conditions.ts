import type { WeatherPoint } from './types';

export const mean = (v: number[]): number => v.reduce((a, b) => a + b, 0) / v.length;

export type PressureHint = 'high' | 'low' | 'ok';

/**
 * Reads the tread temperatures of one tyre (left, centre, right): a centre much hotter than the edges
 * means too much pressure, a centre cooler than the edges too little.
 */
export function pressureHint(tread: number[], tolerance = 5): PressureHint {
  const edges = (tread[0] + tread[2]) / 2;
  const d = tread[1] - edges;
  return d > tolerance ? 'high' : d < -tolerance ? 'low' : 'ok';
}

/** Tyre pressure in bar from kPa. */
export const bar = (kpa: number): number => kpa / 100;

export interface Spark {
  path: string;
  min: number;
  max: number;
}

/**
 * A line through the given values scaled into a width x height box, or null when there are fewer than two.
 * The vertical range is padded so that a flat series still draws a line in the middle.
 */
export function spark(values: (number | null)[], width: number, height: number): Spark | null {
  const pts = values.map((v, i) => [i, v] as const).filter((p): p is readonly [number, number] => p[1] != null);
  if (pts.length < 2) return null;
  const lo = Math.min(...pts.map((p) => p[1]));
  const hi = Math.max(...pts.map((p) => p[1]));
  const span = Math.max(hi - lo, 1);
  const last = values.length - 1;
  const path = pts
    .map(([i, v], k) => {
      const x = (i / last) * width;
      const y = height - ((v - lo) / span) * (height - 4) - 2;
      return `${k === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(' ');
  return { path, min: lo, max: hi };
}

/** True when the last points of the history show rain or a wet track. */
export function isWet(rain: number | null, wetness: number | null): boolean {
  return (rain ?? 0) > 0.05 || (wetness ?? 0) > 0.1;
}

/** Change of a series over its history in the same unit, or null with fewer than two values. */
export function change(points: WeatherPoint[], key: 'track' | 'ambient'): number | null {
  const v = points.map((p) => p[key]).filter((x): x is number => x != null);
  return v.length < 2 ? null : v[v.length - 1] - v[0];
}

/** Temperatures in °C that bound the working range of a tyre: colder is blue, hotter is red. */
export interface TempWindow {
  cold: number;
  low: number;
  high: number;
  hot: number;
}

export const SLICK_WINDOW: TempWindow = { cold: 60, low: 80, high: 100, hot: 120 };

const lerp = (a: number, b: number, f: number) => Math.round(a + (b - a) * f);

/**
 * Colour of a tyre temperature: blue below the window, green inside it, red above it, blending in between.
 * Returns an hsl() string so it works on both themes.
 */
export function tempColor(temp: number, w: TempWindow = SLICK_WINDOW): string {
  // hue 215 = blue, 130 = green, 0 = red
  let hue: number;
  if (temp <= w.cold) hue = 215;
  else if (temp < w.low) hue = lerp(215, 130, (temp - w.cold) / (w.low - w.cold));
  else if (temp <= w.high) hue = 130;
  else if (temp < w.hot) hue = lerp(130, 0, (temp - w.high) / (w.hot - w.high));
  else hue = 0;
  return `hsl(${hue} 70% 45%)`;
}

/** A working range for the tyres whose compound name contains ``name`` (case does not matter). */
export interface TyreRange {
  name: string;
  low: number;
  high: number;
}

/** Starting values; the real ranges depend on the tyre model of the car, so they can be edited in the options. */
export const DEFAULT_RANGES: TyreRange[] = [
  { name: 'soft', low: 85, high: 105 },
  { name: 'medium', low: 80, high: 100 },
  { name: 'hard', low: 75, high: 95 },
  { name: 'wet', low: 50, high: 70 },
];

/** The range of tyres whose compound matches none of the named ranges. */
export const FALLBACK_RANGE = { low: SLICK_WINDOW.low, high: SLICK_WINDOW.high };

/** Colours run from blue to red over this many degrees outside the range. */
const MARGIN = 20;

export const windowOf = (low: number, high: number): TempWindow => ({
  cold: low - MARGIN,
  low,
  high,
  hot: high + MARGIN,
});

/** The range for a compound name: the first named range it contains, else the fallback. */
export function rangeFor(
  compound: string | undefined,
  ranges: TyreRange[],
  fallback: { low: number; high: number } = FALLBACK_RANGE,
): { name: string | null; low: number; high: number } {
  const c = (compound ?? '').toLowerCase();
  const hit = c ? ranges.find((r) => r.name.trim() !== '' && c.includes(r.name.trim().toLowerCase())) : undefined;
  return hit ? { name: hit.name, low: hit.low, high: hit.high } : { name: null, ...fallback };
}
