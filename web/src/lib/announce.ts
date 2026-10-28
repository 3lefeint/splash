import type { Alert, Level } from './strategy';
import { currentEntry, nextEntry } from './plan';
import type { State } from './types';

/** Something to say out loud and to show in the banner while it holds. */
export interface Announcement {
  id: string;
  level: Level;
  /** i18n key and its parameters. */
  key: string;
  params?: Record<string, string | number>;
}

/** Alerts that are worth a sound. Tyre temperatures and rival notes stay on the notes tile. */
const LOUD = /^(wet-now|wet-prepare|fcy|damage|detached|overheating|penalties|agent-)/;

/** Seconds before the end of the stint at which the driver change is announced. */
export const SWAP_WARNINGS = [300, 60] as const;

/** The announcements that hold in the given state; the player speaks every one that is new. */
export function announcements(s: State | null | undefined, etNow: number, alerts: Alert[]): Announcement[] {
  if (!s) return [];
  const out: Announcement[] = alerts.filter((a) => LOUD.test(a.id)).map((a) => ({ id: a.id, level: a.level, key: a.key, params: a.params }));

  const race = s.race;
  if (race && race.stops_remaining > 0 && race.laps_to_stop <= 2) {
    out.push({ id: `stop-${race.laps_to_stop}`, level: race.laps_to_stop === 0 ? 'bad' : 'warn', key: 'say.stopSoon', params: { n: race.laps_to_stop } });
  }

  const cur = currentEntry(s);
  const next = nextEntry(s);
  if (cur && next && !next.reserve && next.driver !== cur.driver) {
    const left = cur.end - etNow;
    for (const limit of SWAP_WARNINGS) {
      // only the tightest warning that applies, so one swap is not announced twice at the same time
      const tighter = SWAP_WARNINGS.filter((l) => l < limit && left <= l).length > 0;
      if (left <= limit && !tighter) {
        out.push({ id: `swap-${limit}`, level: limit <= 60 ? 'bad' : 'warn', key: 'say.swapSoon', params: { min: Math.max(Math.round(left / 60), 1), name: next.driver } });
      }
    }
  }
  return out;
}
