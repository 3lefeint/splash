import { describe, expect, it } from 'vitest';
import { deltaColor, heat, heatColor, hottest, sections, wearColor } from './wear';
import type { WearStats } from './types';

const stats = (over: Partial<WearStats> = {}): WearStats => ({
  segments: 3, laps: 6, per_lap: [], per_wheel: [0, 0, 0, 0], trend_pct: null,
  recent: [[1, 2, 3], [3, 2, 1], [0, 0, 0], [2, 2, 2]], ...over,
});

describe('sections', () => {
  it('picks one wheel or averages all', () => {
    expect(sections(stats().recent, '0')).toEqual([1, 2, 3]);
    expect(sections(stats().recent, 'all')).toEqual([1.5, 1.5, 1.5]);
    expect(sections([], 'all')).toEqual([]);
  });
});

describe('heat', () => {
  it('has nothing to paint without figures or when switched off', () => {
    expect(heat(undefined, 'wear', 'all')).toBeNull();
    expect(heat(stats(), 'off', 'all')).toBeNull();
  });
  it('scales the wear view by its largest value', () => {
    expect(heat(stats(), 'wear', '0')).toEqual({ values: [1, 2, 3], max: 3 });
  });
  it('needs a base for the change view and compares against it', () => {
    expect(heat(stats(), 'delta', '0')).toBeNull();
    const h = heat(stats({ base: [[1, 1, 1], [1, 1, 1], [1, 1, 1], [1, 1, 1]] }), 'delta', '0');
    expect(h).toEqual({ values: [0, 1, 2], max: 2 });
  });
});

describe('colours', () => {
  it('runs from calm to hot and clamps', () => {
    expect(heatColor(0, 1)).toBe('rgb(58 80 104)');
    expect(heatColor(1, 1)).toBe('rgb(255 92 108)');
    expect(heatColor(5, 1)).toBe(heatColor(1, 1));
    expect(heatColor(-1, 1)).toBe(heatColor(0, 1));
  });
  it('shows less wear green and more wear red', () => {
    expect(deltaColor(-1, 1)).toBe('rgb(61 220 132)');
    expect(deltaColor(1, 1)).toBe('rgb(255 92 108)');
    expect(deltaColor(0, 1)).toBe('rgb(58 80 104)');
  });
  it('finds the hottest section', () => {
    expect(hottest([1, 5, 2])).toBe(1);
    expect(hottest([])).toBe(-1);
  });
});

describe('wearColor', () => {
  it('goes from green over orange to red', () => {
    expect(wearColor(1)).toBe('hsl(130 75% 45%)');
    expect(wearColor(0.5)).toBe('hsl(30 75% 45%)');
    expect(wearColor(0)).toBe('hsl(0 75% 45%)');
  });

  it('stays in range', () => {
    expect(wearColor(1.4)).toBe(wearColor(1));
    expect(wearColor(-1)).toBe(wearColor(0));
  });
});
