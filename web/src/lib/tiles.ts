import type { Item } from './grid';

/** How a tile lays out its content: as it is, in one line, or just the value. */
export type TileMode = 'normal' | 'compact' | 'value';

/** What a tile of the dashboard remembers besides its place: stored with the layout in the browser. */
export interface TileOptions {
  mode?: TileMode;
  /** Lines switched off. */
  hide?: string[];
  /** Columns of the grid inside the tile (pit set-ups). */
  columns?: number;
  /** Place of the buttons inside the tile by name (pit set-ups). */
  grid?: Item[];
}

export const MODES: TileMode[] = ['normal', 'compact', 'value'];

/** The lines of a tile that can be switched off. */
export const TILE_LINES: Record<string, string[]> = {
  next: ['lapsToStop'],
  fuel: ['capacity', 'perLap'],
  stops: ['lapsLeft', 'saveTarget', 'saveDelta'],
  driver: ['laps'],
  opponents: ['trend'],
  plan: ['next', 'deviation'],
};

/** Tiles with a gear: those with lines to switch off (they also have the modes) and those with controls of their own. */
export const hasGear = (id: string): boolean => id in TILE_LINES || id === 'map' || id === 'setups';

export const MAX_COLUMNS = 4;

function validItem(x: unknown): x is Item {
  if (!x || typeof x !== 'object') return false;
  const i = x as Record<string, unknown>;
  return (
    typeof i.id === 'string' &&
    [i.x, i.y, i.w, i.h].every((n) => Number.isInteger(n)) &&
    (i.x as number) >= 0 &&
    (i.y as number) >= 0 &&
    (i.w as number) >= 1 &&
    (i.h as number) >= 1
  );
}

/** Takes what was stored and keeps only what is valid for the known tiles. */
export function cleanOptions(raw: unknown, known: Set<string>): Record<string, TileOptions> {
  const out: Record<string, TileOptions> = {};
  if (!raw || typeof raw !== 'object') return out;
  for (const [id, v] of Object.entries(raw as Record<string, unknown>)) {
    if (!known.has(id) || !v || typeof v !== 'object') continue;
    const o = v as Record<string, unknown>;
    const clean: TileOptions = {};
    if (o.mode === 'compact' || o.mode === 'value') clean.mode = o.mode;
    if (Array.isArray(o.hide)) {
      const lines = TILE_LINES[id] ?? [];
      const hide = o.hide.filter((l): l is string => typeof l === 'string' && lines.includes(l));
      if (hide.length) clean.hide = hide;
    }
    if (Number.isInteger(o.columns) && (o.columns as number) >= 1 && (o.columns as number) <= MAX_COLUMNS) {
      clean.columns = o.columns as number;
    }
    if (Array.isArray(o.grid)) clean.grid = o.grid.filter(validItem).map(({ id, x, y, w, h }) => ({ id, x, y, w, h }));
    out[id] = clean;
  }
  return out;
}
