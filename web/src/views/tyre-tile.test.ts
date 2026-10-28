import { beforeEach, describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import TyreTemps from '../components/TyreTemps.svelte';
import { settings } from '../lib/settings.svelte';
import { live } from '../lib/store';
import type { State } from '../lib/types';

const tyres = {
  compound_front: 'Medium', compound_rear: 'Medium',
  surface: [[80, 90, 100], [80, 90, 100], [80, 90, 100], [80, 90, 100]],
  inner: [[85, 85, 85], [85, 85, 85], [85, 85, 85], [85, 85, 85]],
  pressure: [200, 200, 200, 200], brake: [400, 400, 400, 400], water: 90, oil: 100,
};
const withTyres = (): State => ({ ready: true, et: 1, time_left: 2, opponents: { ahead: null, behind: null }, params: {}, overrides: {}, tyres, car: { wear: [0.9, 0.9, 0.8, 0.8] } }) as unknown as State;

beforeEach(() => {
  live.state = withTyres();
});

describe('the tyre temperature tile', () => {
  for (const lang of ['de', 'en'] as const) {
    it(`shows the four tyres and no info text beside them (${lang})`, () => {
      settings.lang = lang;
      const out = render(TyreTemps).body;
      for (const name of ['FL', 'FR', 'RL', 'RR']) expect(out).toContain(`>${name}</text>`);
      expect(out.match(/<text/g)?.length).toBe(20); // four tyres, five texts each
      expect(out).not.toMatch(/Range|Bereich|Brakes|Bremsen|Water|Wasser|Medium|pressure|Druck|class="info/);
    });
  }
  it('says there is no data without tyre temperatures', () => {
    live.state = { ...withTyres(), tyres: undefined } as State;
    expect(render(TyreTemps).body).toContain('class="muted"');
  });
});
