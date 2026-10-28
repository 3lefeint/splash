import { beforeEach, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import TrackMap from '../components/TrackMap.svelte';
import { settings } from '../lib/settings.svelte';
import { live } from '../lib/store';
import { trackMaps } from '../lib/trackmaps.svelte';
import type { State } from '../lib/types';

const outline = { track: 'monza', length: 5000, points: Array.from({ length: 100 }, (_, i) => [Math.cos((i / 100) * 6.283) * 500, Math.sin((i / 100) * 6.283) * 300]) };
const field = [{ car_id: 0, vehicle_name: 'me', driver: 'a', class: 'GT3', place: 3, lap_dist: 4000, in_pits: false, ours: true }];
const rejoin = (over: Record<string, unknown> = {}) => ({ loss: 60, position: 5, lost: 2, passed_by: [], also_stop: [], window: [], best_in: 0, incomplete: false, lap_dist: 1000, ...over });
const state = (rj: unknown, pitExit: [number, number] = [10, 20]): State =>
  ({ ready: true, et: 1, time_left: 2, track: 'monza', track_length: 5000, opponents: { ahead: null, behind: null }, params: {}, overrides: {}, field,
    sections: { pits: { entry: [0, 0], exit: pitExit, boxes: [] } }, strategy: { rivals: [], rejoin: rj } }) as unknown as State;

/** The centre of the ghost circle, or null when there is none. */
function ghost(s: State): [number, number] | null {
  live.state = s;
  const out = render(TrackMap).body;
  const m = out.match(/<circle cx="([-\d.]+)" cy="([-\d.]+)"[^>]*class="ghost/);
  return m ? [Number(m[1]), Number(m[2])] : null;
}

beforeEach(() => {
  live.state = null;
  trackMaps.map = outline as never;
  settings.lang = 'en';
  settings.mapGhost = true;
});

describe('the rejoin marker on the map', () => {
  it('sits at the lap distance of the rejoin point and shows the place', () => {
    live.state = state(rejoin());
    expect(render(TrackMap).body).toContain('Rejoin P5');
    expect(ghost(state(rejoin()))).not.toBeNull();
  });
  it('moves with the rejoin point, that is with the car and with the time lost', () => {
    const a = ghost(state(rejoin({ lap_dist: 1000 })))!;
    const b = ghost(state(rejoin({ lap_dist: 1500 })))!;
    const c = ghost(state(rejoin({ lap_dist: 3500 })))!;
    expect(b).not.toEqual(a);
    expect(c).not.toEqual(a);
    expect(c).not.toEqual(b);
  });
  it('does not depend on the pit exit', () => {
    expect(ghost(state(rejoin(), [10, 20]))).toEqual(ghost(state(rejoin(), [400, -250])));
  });
  it('wraps: the start of the lap and its end are the same place', () => {
    const start = ghost(state(rejoin({ lap_dist: 0 })))!;
    const end = ghost(state(rejoin({ lap_dist: 5000 })))!;
    expect(Math.abs(start[0] - end[0])).toBeLessThan(1);
    expect(Math.abs(start[1] - end[1])).toBeLessThan(1);
  });
  it('is not drawn without a lap distance, and not with the setting off', () => {
    expect(ghost(state(rejoin({ lap_dist: null })))).toBeNull();
    const noKey = rejoin();
    delete (noKey as Record<string, unknown>).lap_dist;
    expect(ghost(state(noKey))).toBeNull();
    settings.mapGhost = false;
    expect(ghost(state(rejoin()))).toBeNull();
  });
});
