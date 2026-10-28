import { beforeEach, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { Component } from 'svelte';
import AlertsPanel from '../components/AlertsPanel.svelte';
import RivalsPanel from '../components/RivalsPanel.svelte';
import TrackMap from '../components/TrackMap.svelte';
import { corners } from '../lib/corners.svelte';
import { settings } from '../lib/settings.svelte';
import { live } from '../lib/store';
import { trackMaps } from '../lib/trackmaps.svelte';
import type { State } from '../lib/types';

const html = (c: Component<any>) => render(c).body;

const car = { car_id: 0, vehicle_name: 'me', driver: 'a', class: 'GT3', place: 2, lap: 1, fuel: 50, fuel_capacity: 90, wear: [], pit_state: 0 };
const rival = (id: number, over: Record<string, unknown> = {}) => ({
  car_id: id, vehicle_name: `r${id}`, driver: `Rival ${id}`, class: 'GT3', place: id + 1, gap: -3, in_pits: false, stops: 0, stint_laps: 5, fuel: 40, wear: [1, 1, 1, 1], pace: null, rate: null, ...over,
});
const field = (over: Record<string, unknown> = {}) => [
  { car_id: 0, vehicle_name: 'me', driver: 'a', class: 'GT3', place: 2, lap_dist: 400, in_pits: false, ours: true, ...over },
  { car_id: 1, vehicle_name: 'r1', driver: 'Rival 1', class: 'GT3', place: 3, lap_dist: 740, in_pits: false, ours: false, probable_cause: true },
  { car_id: 2, vehicle_name: 'r2', driver: 'Rival 2', class: 'GT3', place: 4, lap_dist: 3000, in_pits: false, ours: false },
];
const yellowState = (over: Record<string, unknown> = {}): State =>
  ({
    ready: true, et: 10, time_left: 100, track: 'monza', track_length: 5000, opponents: { ahead: null, behind: null }, params: {}, overrides: {},
    car, field: field(), yellow_sectors: [1],
    strategy: { rivals: [rival(1, { probable_cause: true }), rival(2)] },
    ...over,
  }) as unknown as State;

const outline = { track: 'monza', length: 5000, points: Array.from({ length: 100 }, (_, i) => [Math.cos((i / 100) * 6.283) * 500, Math.sin((i / 100) * 6.283) * 300]) };

beforeEach(() => {
  live.state = null;
  trackMaps.map = outline as never;
  corners.list = [{ name: 'Curva Grande', from: 900, to: 1000, label: null }];
  settings.yellowAhead = true;
  settings.yellowMarkers = true;
});

describe('the yellow alert setting', () => {
  it('shows the line with distance and corner with the setting on and nothing with it off', () => {
    live.state = yellowState();
    expect(html(AlertsPanel)).toContain('Gelb in 340 m, vor Curva Grande');
    settings.yellowAhead = false;
    expect(html(AlertsPanel)).not.toContain('Gelb in');
  });
  it('says the cause is unknown without a candidate, and never names the sector', () => {
    live.state = yellowState({ field: field().map((c) => ({ ...c, probable_cause: false })) });
    const out = html(AlertsPanel);
    expect(out).toContain('Gelb, Ursache unbekannt');
    expect(out).not.toContain('Sektor');
  });
});

describe('the yellow marker setting', () => {
  it('marks the probable cause on the map with the setting on, no sector colouring, no ring on our car', () => {
    live.state = yellowState({ field: field({ probable_cause: true }) });
    const out = html(TrackMap);
    expect(out.match(/class="cause /g)?.length).toBe(1);
    expect(out).not.toContain('yellowsector');
    expect(out).not.toContain('yellowcar');
  });
  it('shows none of it with the setting off', () => {
    live.state = yellowState();
    settings.yellowMarkers = false;
    const out = html(TrackMap);
    expect(out).not.toContain('class="cause');
    expect(html(RivalsPanel)).not.toContain('class="cause');
  });
  it('marks the cause in the rivals list and leaves our row plain', () => {
    live.state = yellowState();
    const out = html(RivalsPanel);
    expect(out).toContain('class="cause');
    expect(out).toContain('<tr class="me svelte-');
  });
});

describe('the two settings do not depend on each other', () => {
  it('alert on and markers off, and the other way round', () => {
    live.state = yellowState();
    settings.yellowMarkers = false;
    expect(html(AlertsPanel)).toContain('Gelb in 340 m');
    expect(html(TrackMap)).not.toContain('class="cause');
    settings.yellowMarkers = true;
    settings.yellowAhead = false;
    expect(html(AlertsPanel)).not.toContain('Gelb in');
    expect(html(TrackMap)).toContain('class="cause');
  });
});

describe('missing and null fields', () => {
  it('do not crash the components', () => {
    for (const over of [
      { yellow_sectors: null, field: null },
      { yellow_sectors: undefined },
      { yellow_sectors: [1, 2, 3], track_length: 0 },
      { yellow_sectors: [7, -1] },
    ]) {
      live.state = yellowState(over);
      for (const c of [AlertsPanel, RivalsPanel, TrackMap]) expect(() => html(c)).not.toThrow();
      live.receive(JSON.stringify(yellowState(over)));
      for (const c of [AlertsPanel, RivalsPanel, TrackMap]) expect(() => html(c)).not.toThrow();
    }
  });
});
