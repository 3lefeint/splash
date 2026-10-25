import { describe, expect, it } from 'vitest';
import { trendTone } from './opponents';

describe('trendTone', () => {
  it('closing on the car ahead is good, the car behind closing is bad', () => {
    expect(trendTone('ahead', -0.4)).toBe('good');
    expect(trendTone('behind', -0.4)).toBe('bad');
    expect(trendTone('ahead', 0.4)).toBe('bad');
    expect(trendTone('behind', 0.4)).toBe('good');
  });
  it('small or missing trends are neutral', () => {
    expect(trendTone('ahead', 0.02)).toBe('neutral');
    expect(trendTone('behind', null)).toBe('neutral');
  });
});
