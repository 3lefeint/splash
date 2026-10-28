import { describe, expect, it } from 'vitest';
import { DEFAULT_RANGES, SLICK_WINDOW, bar, change, isWet, mean, pressureHint, rangeFor, spark, tempColor, windowOf } from './conditions';

describe('pressureHint', () => {
  it('compares the centre with the edges', () => {
    expect(pressureHint([80, 90, 82])).toBe('high');
    expect(pressureHint([88, 80, 90])).toBe('low');
    expect(pressureHint([85, 86, 84])).toBe('ok');
    expect(pressureHint([80, 84, 80], 3)).toBe('high');
  });
});

describe('spark', () => {
  it('needs two values and ignores gaps', () => {
    expect(spark([1], 100, 20)).toBeNull();
    expect(spark([null, 5, null], 100, 20)).toBeNull();
    const s = spark([10, null, 20], 100, 20)!;
    expect(s.min).toBe(10);
    expect(s.max).toBe(20);
    expect(s.path).toBe('M0.0 18.0 L100.0 2.0');
  });
  it('draws a flat series as a line', () => {
    expect(spark([5, 5], 10, 10)!.path).toBe('M0.0 8.0 L10.0 8.0');
  });
});

describe('helpers', () => {
  it('computes means, bars and wetness', () => {
    expect(mean([1, 2, 3])).toBe(2);
    expect(bar(180)).toBe(1.8);
    expect(isWet(0, 0)).toBe(false);
    expect(isWet(0.2, 0)).toBe(true);
    expect(isWet(null, 0.3)).toBe(true);
  });
  it('reports the change over the history', () => {
    const p = (track: number | null) => ({ et: 0, ambient: null, track, rain: null, wetness: null });
    expect(change([p(30), p(null), p(33)], 'track')).toBe(3);
    expect(change([p(30)], 'track')).toBeNull();
  });
});

describe('tempColor', () => {
  it('is blue when cold, green in the window and red when hot', () => {
    expect(tempColor(20)).toBe('hsl(215 70% 45%)');
    expect(tempColor(SLICK_WINDOW.low)).toBe('hsl(130 70% 45%)');
    expect(tempColor(90)).toBe('hsl(130 70% 45%)');
    expect(tempColor(SLICK_WINDOW.high)).toBe('hsl(130 70% 45%)');
    expect(tempColor(150)).toBe('hsl(0 70% 45%)');
  });

  it('blends between the zones', () => {
    expect(tempColor(70)).toBe('hsl(173 70% 45%)');
    expect(tempColor(110)).toBe('hsl(65 70% 45%)');
  });
});

describe('rangeFor', () => {
  it('takes the first named range the compound contains', () => {
    expect(rangeFor('GT3 Medium', DEFAULT_RANGES)).toEqual({ name: 'medium', low: 80, high: 100 });
    expect(rangeFor('Wet', DEFAULT_RANGES).low).toBe(50);
  });

  it('falls back for unknown or missing compounds', () => {
    expect(rangeFor('Prime', DEFAULT_RANGES)).toEqual({ name: null, low: 80, high: 100 });
    expect(rangeFor(undefined, DEFAULT_RANGES).name).toBeNull();
    expect(rangeFor('Slick', [{ name: ' ', low: 1, high: 2 }]).name).toBeNull();
    expect(rangeFor('Prime', [], { low: 70, high: 90 })).toEqual({ name: null, low: 70, high: 90 });
  });

  it('widens the colour window around the range', () => {
    expect(windowOf(85, 105)).toEqual({ cold: 65, low: 85, high: 105, hot: 125 });
  });
});
