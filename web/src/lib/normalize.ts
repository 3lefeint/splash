import type { State } from './types';

/** Turns a missing list into an empty one: the backend sends [] for them, but a null or a missing field from an older
 * backend, a cached state or a gap must not take the page down with a TypeError on .map or .length. */
function list<T extends object, K extends keyof T>(obj: T | null | undefined, key: K): void {
  if (obj && (obj[key] === null || obj[key] === undefined)) (obj[key] as unknown) = [];
}

/** Makes a state from the stream or the cache safe for the components. It changes the object it is given. */
export function normalizeState(s: State): State {
  if (!s || typeof s !== 'object') return s;
  s.opponents = s.opponents ?? { ahead: null, behind: null };
  s.overrides = s.overrides ?? {};
  s.yellow_sectors = s.yellow_sectors ?? [];
  // Optional lists: absent is fine (the components use ?.), null is not.
  for (const key of ['field', 'agents'] as const) if (s[key] === null) (s[key] as unknown) = undefined;

  list(s.car, 'wear');
  list(s.stint_plan, 'entries');
  list(s.pits, 'rows');
  list(s.weather, 'history');
  list(s.fit, 'excluded');

  const w = s.wear_stats;
  if (w) {
    list(w, 'recent');
    list(w, 'per_lap');
    list(w, 'per_wheel');
    if (w.base === null) w.base = undefined;
  }

  const st = s.strategy;
  if (st) {
    list(st, 'rivals');
    for (const r of st.rivals) list(r, 'wear');
    const rj = st.rejoin;
    if (rj) {
      list(rj, 'window');
      list(rj, 'passed_by');
      list(rj, 'also_stop');
    }
  }

  const sec = s.sections;
  if (sec) {
    list(sec.ahead, 'delta');
    list(sec.behind, 'delta');
    list(sec.pits, 'boxes');
  }
  return s;
}
