// Layout arithmetic for the movable tiles of the overview. Positions are in grid cells.

export interface Item {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export const COLS = 12;

const overlaps = (a: Item, b: Item): boolean =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** Moves every item up as far as it goes without touching another one. Keeps the relative order. */
export function compact(items: Item[]): Item[] {
  return settle([], items);
}

/**
 * Places the item with the given id at a new position and size. The other items make room: they keep their
 * order and slide down below it where needed, and up into free space.
 */
export function place(items: Item[], id: string, to: Partial<Omit<Item, 'id'>>, cols = COLS): Item[] {
  const cur = items.find((i) => i.id === id);
  if (!cur) return items;
  const w = clamp(to.w ?? cur.w, 1, cols);
  const moved: Item = {
    id,
    w,
    h: Math.max(to.h ?? cur.h, 1),
    x: clamp(to.x ?? cur.x, 0, cols - w),
    y: Math.max(to.y ?? cur.y, 0),
  };
  // The others are laid out in their current order; items at or below the moved one's top yield to it.
  const rest = items.filter((i) => i.id !== id);
  return settle([moved], rest).sort((a, b) => order(items, a.id) - order(items, b.id));
}

/** Total number of rows used. */
export function rows(items: Item[]): number {
  return items.reduce((m, i) => Math.max(m, i.y + i.h), 0);
}

/**
 * Keeps layouts usable after the set of tiles changed: unknown ids and bad numbers are dropped and tiles
 * without a stored position come from the defaults.
 */
export function reconcile(stored: unknown, defaults: Item[], cols = COLS): Item[] {
  const known = new Map(defaults.map((d) => [d.id, d]));
  const have = new Map<string, Item>();
  if (Array.isArray(stored)) {
    for (const s of stored) {
      const d = known.get((s as Item)?.id);
      const ok = d && [s.x, s.y, s.w, s.h].every((n: unknown) => Number.isInteger(n) && (n as number) >= 0);
      if (d && ok && s.w >= 1 && s.h >= 1 && s.x + s.w <= cols) have.set(d.id, { id: d.id, x: s.x, y: s.y, w: s.w, h: s.h });
    }
  }
  const out = defaults.map((d) => have.get(d.id) ?? d);
  return compact(out);
}

function settle(fixed: Item[], others: Item[]): Item[] {
  const placed = [...fixed];
  const sorted = [...others].sort((a, b) => a.y - b.y || a.x - b.x);
  for (const item of sorted) {
    const next = { ...item, y: 0 };
    while (placed.some((p) => overlaps(p, next))) next.y++;
    placed.push(next);
  }
  return placed;
}

const order = (items: Item[], id: string) => items.findIndex((i) => i.id === id);
const clamp = (n: number, lo: number, hi: number) => Math.min(Math.max(n, lo), hi);

/**
 * The places of named buttons on a grid of ``cols`` columns: stored places are kept (cut to the columns), buttons
 * without one get 1 by 1 in the first free cell, buttons that are gone are dropped, and gaps are closed.
 */
export function fitItems(stored: Item[] | undefined, ids: string[], cols: number): Item[] {
  const wanted = new Set(ids);
  const out: Item[] = [];
  for (const s of stored ?? []) {
    if (!wanted.has(s.id) || out.some((o) => o.id === s.id)) continue;
    const w = clamp(s.w, 1, cols);
    out.push({ id: s.id, w, h: Math.max(s.h, 1), x: clamp(s.x, 0, cols - w), y: Math.max(s.y, 0) });
  }
  for (const id of ids) {
    if (out.some((o) => o.id === id)) continue;
    const item: Item = { id, x: 0, y: 0, w: 1, h: 1 };
    search: for (let y = 0; ; y++) {
      for (let x = 0; x < cols; x++) {
        const at = { ...item, x, y };
        if (!out.some((o) => overlaps(o, at))) {
          out.push(at);
          break search;
        }
      }
    }
  }
  // Stored places may overlap after the columns shrank: the later ones yield.
  return compact(out).sort((a, b) => ids.indexOf(a.id) - ids.indexOf(b.id));
}

/** Height in pixels of one row when ``rows`` rows share ``height`` pixels with a gap between them, but not below ``min``. */
export function rowPixels(height: number, rows: number, gap: number, min: number, fallback: number): number {
  if (height <= 0 || rows <= 0) return fallback;
  return Math.max((height + gap) / rows - gap, min);
}
