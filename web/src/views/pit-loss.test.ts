import { beforeEach, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import PitsPanel from '../components/PitsPanel.svelte';
import SettingsView from './SettingsView.svelte';
import { applySuggestion, differs } from '../lib/pits';
import { dashboardLayout } from '../lib/layout.svelte';
import { settings } from '../lib/settings.svelte';
import { live } from '../lib/store';
import type { Params, State } from '../lib/types';

const html = (c: any) => render(c).body;

const params: Params = { track: 'monza', car: 'me', pit_through_green: 25, pit_through_fcy: 15, refuel_rate: 2, tyre_change: 17, driver_swap: 10, fuel_effect: 0, deg_per_lap: 0, save_cost: 0, reserve: 3 };
const row = (over: Record<string, unknown> = {}) => ({ no: 1, entry: 100, lane: 70, stopped: 40, work: 38, fuel_time: null, through: 30, race_part: 10, net: 20, fuel: 60, tyres: false, swap: false, ...over });
const state = (rows: unknown[], suggest: unknown): State =>
  ({ ready: true, et: 1, time_left: 2, track: 'monza', opponents: { ahead: null, behind: null }, params, overrides: {}, car: { vehicle_name: 'me' }, pits: { rows, suggest } }) as unknown as State;

beforeEach(() => {
  settings.lang = 'en';
});

describe('the pit stop tile', () => {
  it('shows gross, race pace part and net of the lane loss in the row', () => {
    live.state = state([row()], { pit_through: 20, refuel_rate: 2, driver_swap: null });
    const out = html(PitsPanel);
    expect(out).toContain('Lane loss');
    expect(out).toContain('20.0 s');
    expect(out).toContain('(30.0 − 10.0)');
  });
  it('shows dashes while the distance of the pit lane is unknown', () => {
    live.state = state([row({ race_part: null, net: null })], null);
    const out = html(PitsPanel);
    expect(out).toMatch(/>– <span class="muted small[^>]*>\(30.0 − –\)/);
  });
  it('suggests no tyre change any more', () => {
    live.state = state([row()], { pit_through: 20, refuel_rate: 2, driver_swap: null });
    dashboardLayout.reset();
    dashboardLayout.toggleLine('pits', 'suggest');
    const out = html(PitsPanel);
    expect(out).not.toContain('Tyre change');
    expect(out).toContain('Through the pit lane');
    dashboardLayout.reset();
  });
  it('keeps the suggestions out of the tile until they are switched on', () => {
    live.state = state([row()], { pit_through: 20, refuel_rate: 2, driver_swap: null });
    dashboardLayout.reset();
    expect(html(PitsPanel)).not.toContain('Through the pit lane');
  });
  it('lists the latest stop first', () => {
    live.state = state([row({ no: 1 }), row({ no: 2 }), row({ no: 3 })], undefined);
    const out = html(PitsPanel);
    const numbers = [...out.matchAll(/<td[^>]*>(\d)<\/td>/g)].map((m) => m[1]);
    expect(numbers).toEqual(['3', '2', '1']);
  });
});

describe('applying a suggestion', () => {
  it('leaves the tyre change as it is', () => {
    const s = { pit_through: 20, refuel_rate: 3, driver_swap: null };
    expect(applySuggestion(params, s).tyre_change).toBe(17);
    expect(applySuggestion(params, s).pit_through_green).toBe(20);
    expect(differs(params, { pit_through: 25, refuel_rate: null, driver_swap: null })).toBe(false);
  });
});

describe('the pit lane loss parameters', () => {
  for (const [lang, label, help] of [
    ['en', 'Pit lane loss, green (s)', 'Time lost against racing past, including braking into the box and accelerating out, without the standing time.'],
    ['de', 'Verlust Boxengasse, Grün (s)', 'Zeit, die gegenüber dem Vorbeifahren verloren geht, einschließlich Anbremsen in die Box und Beschleunigen heraus, ohne die Standzeit.'],
  ] as const) {
    it(`say what is measured and how (${lang})`, () => {
      settings.lang = lang;
      live.state = state([], null);
      const out = html(SettingsView);
      expect(out).toContain(label);
      expect(out).toContain(help);
      expect(out).toMatch(lang === 'en' ? /a stop with only a tyre change shows it/ : /ein Stopp nur mit Reifenwechsel zeigt sie/);
      expect(out).not.toMatch(/drive-through|Durchfahrt, /);
    });
  }
});

describe('no parallel service any more', () => {
  for (const lang of ['en', 'de'] as const) {
    it(`has no setting and no suggestion about it (${lang})`, () => {
      settings.lang = lang;
      live.state = state([row()], { pit_through: 20, refuel_rate: 2, driver_swap: null });
      expect(html(SettingsView).toLowerCase()).not.toMatch(/parallel/);
      expect(html(PitsPanel).toLowerCase()).not.toMatch(/parallel/);
    });
  }
});

describe('the reason for a missing lane loss', () => {
  const missing = (reason: string) => state([row({ race_part: null, net: null, net_missing: reason })], null);
  for (const [lang, cases] of [
    ['en', { entry_dist: 'Pit lane distance not learned yet (entry)', exit_dist: 'Pit lane distance not learned yet (exit)', lane_unknown: 'Pit lane not learned yet', track_length: 'Track length not known yet', lap_time: 'No clean lap time yet', stand_time: 'No standing time measured', in_progress: 'Still in the pit lane' }],
    ['de', { entry_dist: 'Länge der Boxengasse noch nicht gelernt (Einfahrt)', exit_dist: 'Länge der Boxengasse noch nicht gelernt (Ausfahrt)', lane_unknown: 'Boxengasse noch nicht gelernt', track_length: 'Streckenlänge noch nicht bekannt', lap_time: 'Noch keine saubere Rundenzeit', stand_time: 'Keine Standzeit gemessen', in_progress: 'Noch in der Boxengasse' }],
  ] as const) {
    it(`is shown in the stop row (${lang})`, () => {
      settings.lang = lang;
      for (const [reason, text] of Object.entries(cases)) {
        live.state = missing(reason);
        expect(html(PitsPanel)).toContain(text);
      }
    });
  }
  it('shows the numbers when there is a net value', () => {
    settings.lang = 'en';
    live.state = state([row()], null);
    const out = html(PitsPanel);
    expect(out).toContain('(30.0 − 10.0)');
    expect(out).not.toContain('not learned');
  });
});
