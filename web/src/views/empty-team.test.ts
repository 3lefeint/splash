import { beforeEach, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { Component } from 'svelte';
import RivalsPanel from '../components/RivalsPanel.svelte';
import Dashboard from './Dashboard.svelte';
import MapView from './MapView.svelte';
import PlanView from './PlanView.svelte';
import RestView from './RestView.svelte';
import SessionsView from './SessionsView.svelte';
import SettingsView from './SettingsView.svelte';
import TracksView from './TracksView.svelte';
import { live } from '../lib/store';
import type { State } from '../lib/types';

// Server-side rendering runs the template and the derived values of a component without a browser; it throws the same
// TypeError a page would. render() is lazy: the component only runs when .body is read.
const html = (c: Component<any>) => render(c).body;

const emptyTeam = (over: Record<string, unknown> = {}): State =>
  ({ ready: false, et: 0, time_left: 0, opponents: { ahead: null, behind: null }, params: {}, overrides: {}, ...over }) as unknown as State;

const car = { car_id: 0, vehicle_name: 'me', driver: 'a', class: 'GT3', place: 1, lap: 1, fuel: 50, fuel_capacity: 90, fuel_per_lap: null, lap_time: null, wear: [], pit_state: 0 };

/** A ready state in which every list the backend could leave empty is null, as an old or odd backend might send it. */
const allNull = (): State =>
  emptyTeam({
    ready: true,
    track: 'monza',
    track_length: 5000,
    car: { ...car, wear: null },
    race: { laps_to_stop: 5, laps_left: 40, stops_remaining: 2, save_target: null, stint_end: 1000 },
    stint_plan: { entries: null, shortfall: 0, current_deviation: 0, pit_loss: 0 },
    field: null,
    agents: null,
    pits: { rows: null },
    weather: { ambient: 20, track: 25, rain: 0, wetness: 0, dark_cloud: 0, history: null },
    wear_stats: { segments: 50, laps: 3, recent: null, per_lap: null, per_wheel: null, trend_pct: null },
    strategy: {
      rivals: null,
      rejoin: { loss: 30, position: 1, lost: 0, passed_by: null, also_stop: null, window: null, best_in: 0, incomplete: false },
    },
    sections: { segments: 50, laps: 1, ahead: { name: 'x', delta: null, laps: 1 }, pits: { boxes: null } },
  });

const views: [string, Component<any>][] = [
  ['rivals panel', RivalsPanel],
  ['dashboard', Dashboard],
  ['stint plan', PlanView],
  ['map', MapView],
  ['sessions', SessionsView],
  ['settings', SettingsView],
  ['tracks', TracksView],
  ['rest', RestView],
];

beforeEach(() => {
  live.state = null;
});

describe.each(views)('the %s', (_name, view) => {
  it('renders before any state has arrived', () => {
    expect(() => html(view)).not.toThrow();
  });

  it('renders for a team that has no data at all', () => {
    live.state = emptyTeam();
    expect(() => html(view)).not.toThrow();
  });

  it('renders when the state arrives with its lists null', () => {
    live.receive(JSON.stringify(allNull()));
    expect(live.state?.strategy?.rivals).toEqual([]);
    expect(() => html(view)).not.toThrow();
  });

  it('renders even if null lists slip past the normalisation', () => {
    live.state = allNull();
    expect(() => html(view)).not.toThrow();
  });
});

describe('the rivals panel', () => {
  it('shows the other cars of a filled state', () => {
    live.receive(
      JSON.stringify(
        emptyTeam({
          ready: true,
          car,
          strategy: { rivals: [{ car_id: 1, vehicle_name: 'r', driver: 'Rival One', class: 'GT3', place: 2, gap: -3, in_pits: false, stops: 0, stint_laps: 5, fuel: 40, wear: [1, 1, 1, 1], pace: null, rate: null }] },
        }),
      ),
    );
    const out = html(RivalsPanel);
    expect(out).toContain('<table');
    expect(out).toContain('<tr class="me'); // our own row
    expect(out.match(/<tr class="svelte|<tr class="pit/g)?.length).toBe(1); // and the row of the other car
  });
});
