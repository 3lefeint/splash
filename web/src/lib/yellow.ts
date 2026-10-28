import { locate } from './corners';
import type { Corner, FieldCar, State } from './types';

/** The line about a local yellow: where the nearest probable cause ahead is, or that no cause is known. */
export type YellowLine =
  | { kind: 'ahead'; dist: number; corner: { name: string; mode: 'in' | 'before' } | null; others: number }
  | { kind: 'unknown' };

/** The two yellow settings of a saved record; the names before they changed their meaning still count, so a choice survives. */
export const migrateYellow = (saved: { yellowAhead?: boolean; yellowMarkers?: boolean; yellowAlert?: boolean; yellowMap?: boolean } | null | undefined): { yellowAhead: boolean; yellowMarkers: boolean } => ({
  yellowAhead: (saved?.yellowAhead ?? saved?.yellowAlert) !== false,
  yellowMarkers: (saved?.yellowMarkers ?? saved?.yellowMap) !== false,
});

/** The cars that probably cause a local yellow, as the backend marks them; our own car and cars in the pits never. */
export const yellowCauses = (s: State | null | undefined): FieldCar[] => (s?.field ?? []).filter((c) => c.probable_cause && !c.ours && !c.in_pits);

/** Metres from `from` forward along the lap to `to`, 0 up to the lap length. */
const forward = (from: number, to: number, length: number): number => (((to - from) % length) + length) % length;

/**
 * What to tell the driver about a local yellow. A candidate counts as ahead when it is at most half a lap in front of
 * our car (the lap wraps at the start line); the others lie behind and are only drawn on the map. Null when no sector
 * is flagged, while our car is in the pits, or when every candidate lies behind.
 */
export function yellowLine(s: State | null | undefined, corners: Corner[]): YellowLine | null {
  if (!s || (s.yellow_sectors ?? []).length === 0) return null;
  const me = (s.field ?? []).find((c) => c.ours);
  if (me?.in_pits) return null;
  const causes = yellowCauses(s);
  if (causes.length === 0) return { kind: 'unknown' };
  const length = s.track_length ?? 0;
  if (!me || !(length > 0)) return null;
  const ahead = causes
    .map((c) => ({ pos: c.lap_dist, dist: forward(me.lap_dist, c.lap_dist, length) }))
    .filter((c) => c.dist <= length / 2)
    .sort((a, b) => a.dist - b.dist);
  if (ahead.length === 0) return null;
  const where = locate(corners, ahead[0].pos, length);
  const corner = where.current
    ? { name: where.current.name, mode: 'in' as const }
    : where.next
      ? { name: where.next.name, mode: 'before' as const }
      : null;
  return { kind: 'ahead', dist: Math.round(ahead[0].dist / 10) * 10, corner, others: ahead.length - 1 };
}
