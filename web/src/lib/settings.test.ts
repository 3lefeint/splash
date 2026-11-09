import { describe, expect, it } from 'vitest';
import { DEFAULT_BRAKE } from './conditions';
import { settings } from './settings.svelte';

describe('tyre and brake ranges with a team default', () => {
  it('follow the team default until this browser changes them, and reset returns to it', () => {
    settings.resetTyreRanges();
    expect(settings.brakeRange).toEqual(DEFAULT_BRAKE);

    settings.setTeamRanges({ brakeRange: { low: 200, high: 600 }, tyreFallback: { low: 70, high: 95 }, tyreRanges: [{ name: 'Wet', low: 40, high: 60 }] });
    expect(settings.brakeRange).toEqual({ low: 200, high: 600 });
    expect(settings.tyreRanges.map((r) => r.name)).toEqual(['Wet']);

    settings.brakeRange.low = 250;
    settings.saveRanges();
    settings.setTeamRanges({ brakeRange: { low: 100, high: 500 } }); // an own range stays
    expect(settings.brakeRange.low).toBe(250);

    settings.resetTyreRanges();
    expect(settings.brakeRange).toEqual({ low: 100, high: 500 });

    settings.setTeamRanges(null);
    settings.resetTyreRanges();
    expect(settings.brakeRange).toEqual(DEFAULT_BRAKE);
  });

  it('ignore a team default without any valid range', () => {
    settings.resetTyreRanges();
    settings.setTeamRanges({ brakeRange: { low: 'x' } });
    expect(settings.brakeRange).toEqual(DEFAULT_BRAKE);
  });
});
