import { describe, expect, it } from 'vitest';
import { dictionaries, translate } from './i18n';

describe('i18n', () => {
  it('both languages have the same keys', () => {
    expect(Object.keys(dictionaries.de).sort()).toEqual(Object.keys(dictionaries.en).sort());
  });
  it('placeholders match between languages', () => {
    const names = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
    for (const key of Object.keys(dictionaries.en) as (keyof typeof dictionaries.en)[]) {
      expect(names(dictionaries.de[key]), key).toEqual(names(dictionaries.en[key]));
    }
  });
  it('fills placeholders and keeps unknown ones visible', () => {
    expect(translate('en', 'dash.lap', { n: 7 })).toBe('Lap 7');
    expect(translate('de', 'dash.lap', {})).toBe('Runde {n}');
  });
});
