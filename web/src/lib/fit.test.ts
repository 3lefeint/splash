import { describe, expect, it } from 'vitest';
import { excludedByReason, fitSettingsFor } from './fit';
import { normalizeState } from './normalize';
import type { State } from './types';

const fit = (excluded: { lap: number; reason: string }[] | undefined) => ({ excluded });

describe('fitSettingsFor', () => {
  it('always names the minimum, and the setting behind each reason that has one', () => {
    expect(fitSettingsFor(fit([]))).toEqual(['fit_min_laps']);
    expect(fitSettingsFor(fit([{ lap: 1, reason: 'out_lap' }, { lap: 2, reason: 'in_lap' }, { lap: 3, reason: 'missing_data' }]))).toEqual(['fit_min_laps']);
    expect(fitSettingsFor(fit([{ lap: 4, reason: 'too_slow' }, { lap: 5, reason: 'wet' }, { lap: 6, reason: 'fcy' }]))).toEqual([
      'fit_min_laps', 'fit_slow_percent', 'fit_skip_fcy', 'fit_skip_wet',
    ]);
  });
  it('survives a missing list', () => {
    expect(fitSettingsFor(fit(undefined))).toEqual(['fit_min_laps']);
    expect(fitSettingsFor(null)).toEqual(['fit_min_laps']);
  });
});

describe('excludedByReason', () => {
  it('groups the laps in lap order, the reasons in a fixed order, unknown reasons last', () => {
    const groups = excludedByReason(fit([
      { lap: 9, reason: 'wet' }, { lap: 2, reason: 'too_slow' }, { lap: 7, reason: 'too_slow' }, { lap: 1, reason: 'something_new' }, { lap: 3, reason: 'out_lap' },
    ]));
    expect(groups).toEqual([
      { reason: 'too_slow', laps: [2, 7] },
      { reason: 'out_lap', laps: [3] },
      { reason: 'wet', laps: [9] },
      { reason: 'something_new', laps: [1] },
    ]);
  });
  it('gives nothing for an empty or missing list', () => {
    expect(excludedByReason(fit([]))).toEqual([]);
    expect(excludedByReason(undefined)).toEqual([]);
  });
  it('a null list from an older backend is made empty by the normalisation', () => {
    const s = normalizeState({ fit: { laps: 1, excluded: null } } as unknown as State);
    expect(s.fit?.excluded).toEqual([]);
  });
});
