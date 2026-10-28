import { beforeEach, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import ModelPanel from '../components/ModelPanel.svelte';
import SettingsView from './SettingsView.svelte';
import { settings } from '../lib/settings.svelte';
import { live } from '../lib/store';
import type { State } from '../lib/types';

const html = (c: any) => render(c).body;

const params = { track: 'monza', car: 'me', pit_through_green: 0, pit_through_fcy: 0, refuel_rate: 0, tyre_change: 0, driver_swap: 0, fuel_effect: 0, deg_per_lap: 0, save_cost: 0, reserve: 3 };
const car = { car_id: 0, vehicle_name: 'me', driver: 'a', class: 'GT3', place: 1, lap: 1, fuel: 50, fuel_capacity: 90, wear: [], pit_state: 0 };
const state = (fit: unknown): State =>
  ({ ready: true, et: 1, time_left: 2, track: 'monza', opponents: { ahead: null, behind: null }, params, overrides: {}, car, fit }) as unknown as State;

beforeEach(() => {
  live.state = null;
});

describe('the settings form', () => {
  for (const [lang, labels] of [
    ['en', ['Minimum clean laps', 'Slow limit (%)', 'Leave out full course yellow laps', 'Leave out wet laps', 'Laps needed before the model is fitted (3 to 20).']],
    ['de', ['Mindestens saubere Runden', 'Langsam-Grenze (%)', 'Gelbphasen-Runden auslassen', 'Nasse Runden auslassen', 'Runden, die es braucht, bevor das Modell geschätzt wird (3 bis 20).']],
  ] as const) {
    it(`has the four lap time model fields with their help text (${lang})`, () => {
      settings.lang = lang;
      live.state = state(undefined);
      const out = html(SettingsView);
      for (const l of labels) expect(out).toContain(l);
      expect(out).toMatch(/type="number"[^>]*min="3"[^>]*max="20"/);
      expect(out).toMatch(/min="2"[^>]*max="30"/);
      expect(out.match(/type="checkbox"/g)?.length).toBeGreaterThanOrEqual(2);
    });
  }
});

describe('the lap time model panel', () => {
  beforeEach(() => {
    settings.lang = 'en';
  });
  it('lists the excluded laps with their reasons and the settings that would change it', () => {
    live.state = state({ laps: 2, reason: 'few_laps', min_laps: 5, base: 0, r2: 0, fuel_effect: null, deg_per_lap: null, excluded: [
      { lap: 1, reason: 'out_lap' }, { lap: 3, reason: 'too_slow' }, { lap: 4, reason: 'wet' }, { lap: 8, reason: 'too_slow' },
    ] });
    const out = html(ModelPanel);
    expect(out).toContain('Not enough clean laps yet (2 of 5)');
    expect(out).toContain('<details');
    expect(out).toContain('Laps that did not count (4)');
    expect(out).toContain('too slow</b>: 3, 8');
    expect(out).toContain('out-lap</b>: 1');
    expect(out).toContain('wet</b>: 4');
    expect(out).toContain('Settings that change this: Minimum clean laps, Slow limit (%), Leave out wet laps.');
    expect(out).not.toContain('full course yellow</b>');
  });
  it('shows no list for an empty or missing list, and none for a fitted model', () => {
    for (const excluded of [[], undefined, null]) {
      live.state = state({ laps: 2, reason: 'few_laps', base: 0, r2: 0, fuel_effect: null, deg_per_lap: null, excluded });
      const out = html(ModelPanel);
      expect(out).toContain('(2 of 8)'); // an older backend sends no minimum
      expect(out).not.toContain('<details');
    }
    live.state = state({ laps: 20, base: 90, r2: 0.9, fuel_effect: 0.03, deg_per_lap: 0.05, excluded: [] });
    expect(html(ModelPanel)).not.toContain('<details');
  });
  it('speaks German too', () => {
    settings.lang = 'de';
    live.state = state({ laps: 1, reason: 'few_laps', min_laps: 5, base: 0, r2: 0, excluded: [{ lap: 2, reason: 'fcy' }] });
    const out = html(ModelPanel);
    expect(out).toContain('Gelbphase</b>: 2');
    expect(out).toContain('Gelbphasen-Runden auslassen');
  });
});

describe('the tyre change field', () => {
  for (const [lang, hint] of [
    ['en', '17 s is the default; enter another value to override it, empty or 0 uses the default.'],
    ['de', 'Standard sind 17 s; ein anderer Wert überschreibt ihn, leer oder 0 nimmt den Standard.'],
  ] as const) {
    it(`shows the default as a hint, a placeholder and the starting value (${lang})`, () => {
      settings.lang = lang;
      live.state = state(undefined);
      const out = html(SettingsView);
      expect(out).toContain(hint);
      expect(out).toContain('placeholder="17"');
      expect(out).toMatch(/step="0.1"[^>]*placeholder="17"[^>]*value="17"|value="17"[^>]*placeholder="17"/);
    });
  }
});
