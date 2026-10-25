import { heat, type Overlay, type Wheel } from './wear';
import type { State } from './types';

/** The other car the time view compares with: the one ahead, the one behind, or our best lap. */
export type Target = 'ahead' | 'behind' | 'best';

export interface Paint {
  /** One value per section of the lap; null where nothing is known. */
  values: (number | null)[];
  /** The value with the strongest colour. */
  max: number;
  /** heat: from quiet to strong; diverging: green below zero, red above. */
  mode: 'heat' | 'diverging';
}

const KMH = 3.6;

const finite = (v: (number | null)[]) => v.filter((x): x is number => x != null);

/** The values to paint for the chosen overlay, or null when there is nothing to show yet. */
export function paintFor(s: State | null | undefined, overlay: Overlay, wheel: Wheel, target: Target): Paint | null {
  if (!s || overlay === 'off') return null;
  if (overlay === 'wear' || overlay === 'delta') {
    const h = heat(s.wear_stats, overlay, wheel);
    return h ? { values: h.values, max: h.max, mode: overlay === 'delta' ? 'diverging' : 'heat' } : null;
  }
  const sec = s.sections;
  if (!sec) return null;
  let values: (number | null)[] | undefined;
  let mode: Paint['mode'] = 'heat';
  switch (overlay) {
    case 'time':
      values = target === 'best' ? sec.best : target === 'ahead' ? sec.ahead?.delta : sec.behind?.delta;
      mode = 'diverging';
      break;
    case 'fuel': {
      // the use hardly differs between sections, so the lowest one is the quiet end of the scale
      const low = sec.fuel ? Math.min(...finite(sec.fuel)) : 0;
      values = sec.fuel?.map((v) => (v == null ? null : v - low));
      break;
    }
    case 'speed':
      // slow sections are the strong ones: they show where the corners are
      values = sec.speed?.map((v) => (v == null ? null : v * KMH));
      if (values) {
        const top = Math.max(...finite(values), 1);
        values = values.map((v) => (v == null ? null : top - v));
      }
      break;
    case 'brake':
      values = sec.brake;
      break;
  }
  if (!values) return null;
  const nums = finite(values);
  if (nums.length === 0) return null;
  const max = overlay === 'brake' ? 1 : Math.max(...nums.map(Math.abs), 1e-9);
  return { values, max, mode };
}

/** Name of the car the time view compares with, for the legend. */
export function targetName(s: State | null | undefined, target: Target): string {
  if (target === 'best') return '';
  return (target === 'ahead' ? s?.sections?.ahead?.name : s?.sections?.behind?.name) ?? '';
}
