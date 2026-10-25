import type { FieldCar, TrackMap } from './types';

export type Point = [number, number];

export interface View {
  viewBox: string;
  /** SVG path of the closed outline. */
  path: string;
  /** Screen position of a world point. */
  project: (p: Point) => Point;
}

/** Screen coordinates of a world point: x to the right, z upwards (north up). */
const screen = ([x, z]: Point): Point => [x, -z];

/** Fits the outline into a view box with a margin; the aspect ratio of the track is kept. */
export function fitView(points: Point[], margin = 0.08): View {
  const pts = points.map(screen);
  const xs = pts.map((p) => p[0]);
  const ys = pts.map((p) => p[1]);
  const minX = Math.min(...xs);
  const minY = Math.min(...ys);
  const w = Math.max(Math.max(...xs) - minX, 1);
  const h = Math.max(Math.max(...ys) - minY, 1);
  const pad = Math.max(w, h) * margin;
  const r = (n: number) => Math.round(n * 10) / 10;
  const path = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${r(p[0])} ${r(p[1])}`).join(' ') + ' Z';
  return {
    viewBox: `${r(minX - pad)} ${r(minY - pad)} ${r(w + 2 * pad)} ${r(h + 2 * pad)}`,
    path,
    project: (p) => screen(p),
  };
}

/** World position at a distance along the lap; the outline is closed and evenly spaced. */
export function pointAt(map: Pick<TrackMap, 'length' | 'points'>, dist: number): Point {
  const n = map.points.length;
  const f = ((((dist / map.length) * n) % n) + n) % n;
  const i = Math.floor(f);
  const t = f - i;
  const a = map.points[i];
  const b = map.points[(i + 1) % n];
  return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
}

/** Signed shortest distance from one lap distance to another on a closed lap. */
export function wrapDelta(from: number, to: number, length: number): number {
  let d = (to - from) % length;
  if (d > length / 2) d -= length;
  if (d < -length / 2) d += length;
  return d;
}

interface Track {
  dist: number; // lap distance at the last update
  speed: number; // metres per second as seen between the last two updates
  at: number; // ms timestamp of the last update
}

/** Moves cars smoothly between the roughly one-per-second updates by continuing with their last speed. */
export class Animator {
  private cars = new Map<string, Track>();

  constructor(
    private length: number,
    /** The longest time a car is extrapolated without a new update. */
    private maxAheadMs = 1500,
  ) {}

  static key(c: FieldCar): string {
    return `${c.car_id}|${c.vehicle_name}`;
  }

  update(field: FieldCar[], now: number, length = this.length): void {
    this.length = length;
    const seen = new Set<string>();
    for (const c of field) {
      const k = Animator.key(c);
      seen.add(k);
      const prev = this.cars.get(k);
      let speed = 0;
      if (prev && now > prev.at) {
        const moved = wrapDelta(prev.dist, c.lap_dist, length);
        speed = moved > 0 ? (moved / (now - prev.at)) * 1000 : 0; // standing or reversing cars stay put
        speed = Math.min(speed, 120); // 430 km/h; guards against a jump after a reset
      }
      this.cars.set(k, { dist: c.lap_dist, speed, at: now });
    }
    for (const k of this.cars.keys()) if (!seen.has(k)) this.cars.delete(k);
  }

  /** Lap distance of a car at the given time, or null for an unknown car. */
  at(c: FieldCar, now: number): number | null {
    const t = this.cars.get(Animator.key(c));
    if (!t) return null;
    const dt = Math.min(Math.max(now - t.at, 0), this.maxAheadMs);
    const d = t.dist + (t.speed * dt) / 1000;
    return ((d % this.length) + this.length) % this.length;
  }
}

const PALETTE = ['#4cc2ff', '#ffb454', '#c58bff', '#3ddc84', '#ff7ab8', '#e5e06a'];

/** A stable colour per class: classes are sorted by name so the colour does not change during a session. */
export function classColor(classes: string[], cls: string): string {
  const sorted = [...new Set(classes)].sort();
  const i = sorted.indexOf(cls);
  return PALETTE[(i < 0 ? 0 : i) % PALETTE.length];
}

/** The start number of a vehicle name such as "BMW M4 GT3 #06", or an empty string. */
export function carNumber(vehicleName: string): string {
  const m = /#\s*(\d+)/.exec(vehicleName);
  return m ? m[1] : '';
}

/** Identity of a car for hiding it on the map; the slot id keeps cars with the same name apart. */
export function hideKey(c: Pick<FieldCar, 'car_id' | 'vehicle_name'>): string {
  return `${c.vehicle_name}|${c.car_id}`;
}
