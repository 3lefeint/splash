import { pointAt, type View } from './trackmap';
import type { Corner, TrackMap } from './types';

export interface Marked {
  name: string;
  from: number;
  to: number;
}

/** The corners that have both ends, in lap order of their start. */
export function marked(corners: Corner[]): Marked[] {
  return corners
    .filter((c): c is Corner & { from: number; to: number } => c.from != null && c.to != null)
    .map((c) => ({ name: c.name, from: c.from, to: c.to }))
    .sort((a, b) => a.from - b.from);
}

const wrap = (d: number, length: number): number => ((d % length) + length) % length;

/** Whether a lap distance lies inside a stretch; a stretch whose end is below its start crosses the start line. */
export function inside(c: Pick<Marked, 'from' | 'to'>, dist: number, length: number): boolean {
  const d = wrap(dist, length);
  return c.from <= c.to ? d >= c.from && d <= c.to : d >= c.from || d <= c.to;
}

export interface Where {
  /** The corner the car is in right now. */
  current: Marked | null;
  /** The next corner that starts ahead of the car. */
  next: Marked | null;
  /** Metres to the start of the next corner. */
  toNext: number | null;
}

export function locate(corners: Corner[], dist: number, length: number): Where {
  const list = marked(corners);
  if (list.length === 0 || length <= 0) return { current: null, next: null, toNext: null };
  const d = wrap(dist, length);
  const current = list.find((c) => inside(c, d, length)) ?? null;
  let next: Marked | null = null;
  let toNext = Infinity;
  for (const c of list) {
    if (c === current) continue;
    const ahead = wrap(c.from - d, length);
    if (ahead < toNext) {
      toNext = ahead;
      next = c;
    }
  }
  return { current, next, toNext: next ? toNext : null };
}

/** Lap distance of the outline point nearest to a world position; the outline is closed and evenly spaced. */
export function distAt(points: [number, number][], length: number, at: [number, number]): number {
  let best = 0;
  let bestD = Infinity;
  points.forEach((p, i) => {
    const d = (p[0] - at[0]) ** 2 + (p[1] - at[1]) ** 2;
    if (d < bestD) {
      bestD = d;
      best = i;
    }
  });
  return (best / points.length) * length;
}

/** Outline point indices of a stretch, end included; follows the closed outline across its seam. */
export function indices(n: number, length: number, from: number, to: number): number[] {
  const a = Math.round((wrap(from, length) / length) * n) % n;
  let b = Math.round((wrap(to, length) / length) * n) % n;
  if (b < a) b += n;
  const out: number[] = [];
  for (let k = a; k <= b; k++) out.push(k % n);
  return out;
}

/** The colour of the corner with this number; neighbours differ clearly. */
export const cornerColor = (i: number): string => `hsl(${(i * 67) % 360} 75% 55%)`;

/** Names, one per line, as corners without marks. Blank lines and repeats of a name already in the list are skipped. */
export function parseNames(text: string, existing: Corner[]): Corner[] {
  const have = new Set(existing.map((c) => c.name.toLowerCase()));
  const out: Corner[] = [];
  for (const line of text.split('\n')) {
    const name = line.trim().slice(0, 40);
    if (!name || have.has(name.toLowerCase())) continue;
    have.add(name.toLowerCase());
    out.push({ name, from: null, to: null, label: null });
  }
  return out;
}

export interface Stretch {
  /** Index in the corner list. */
  i: number;
  corner: Corner;
  /** SVG path of the marked stretch; empty while only the start is marked. */
  d: string;
  /** Screen positions along the marked stretch. */
  pts: [number, number][];
  /** Screen position of the start. */
  start: [number, number];
  /** Screen position of the middle of the stretch, where the name is attached. */
  mid: [number, number];
}

/** The marked stretches of the corners on the outline, in screen coordinates of the view. */
export function stretches(map: Pick<TrackMap, 'length' | 'points'>, view: View, list: Corner[]): Stretch[] {
  return list.flatMap((corner, i): Stretch[] => {
    if (corner.from == null) return [];
    if (corner.to == null) {
      const p = view.project(pointAt(map, corner.from));
      return [{ i, corner, d: '', pts: [p], start: p, mid: p }];
    }
    const pts = indices(map.points.length, map.length, corner.from, corner.to).map((k) => view.project(map.points[k]));
    const d = pts.map((p, k) => `${k === 0 ? 'M' : 'L'}${Math.round(p[0] * 10) / 10} ${Math.round(p[1] * 10) / 10}`).join(' ');
    return [{ i, corner, d, pts, start: pts[0], mid: pts[Math.floor(pts.length / 2)] }];
  });
}

export interface Line {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

/** Rough size of a name drawn in bold at this font size; the text is anchored at its left end on the baseline. */
export function textBox(name: string, x: number, y: number, size: number): { x0: number; y0: number; x1: number; y1: number } {
  return { x0: x, y0: y - size * 0.8, x1: x + name.length * size * 0.62, y1: y + size * 0.25 };
}

/** The line from a name to its stretch: it ends at the point of the stretch nearest to the name and at the point of the name nearest to that. */
export function leader(pts: [number, number][], name: string, x: number, y: number, size: number): Line {
  const b = textBox(name, x, y, size);
  const cx = (b.x0 + b.x1) / 2;
  const cy = (b.y0 + b.y1) / 2;
  let near = pts[0];
  let best = Infinity;
  for (const p of pts) {
    const d = (p[0] - cx) ** 2 + (p[1] - cy) ** 2;
    if (d < best) {
      best = d;
      near = p;
    }
  }
  const clamp = (n: number, lo: number, hi: number) => Math.min(Math.max(n, lo), hi);
  return { x1: near[0], y1: near[1], x2: clamp(near[0], b.x0, b.x1), y2: clamp(near[1], b.y0, b.y1) };
}

/** The corners in the order of the track: by where they start, those not marked yet at the end in the order they were added. */
export function byPosition<T extends Corner>(list: T[]): { corner: T; i: number }[] {
  return list
    .map((corner, i) => ({ corner, i }))
    .sort((a, b) => {
      const x = a.corner.from;
      const y = b.corner.from;
      if (x == null || y == null) return x == null && y == null ? a.i - b.i : x == null ? 1 : -1;
      return x - y || a.i - b.i;
    });
}
