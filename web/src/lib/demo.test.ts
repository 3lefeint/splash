import { describe, expect, it } from 'vitest';
import { demoMap, demoState } from './demo';

describe('demo state', () => {
  it('is a complete ready state with a car, so every tile of the dashboard has something to show', () => {
    const s = demoState();
    expect(s.ready).toBe(true);
    expect(s.car?.driver).toBeTruthy();
    expect(s.race?.stops_remaining).toBeGreaterThan(0);
    expect(s.tyres?.brake).toHaveLength(4);
    expect(s.tyres?.surface).toHaveLength(4);
    expect(s.field?.some((c) => c.ours)).toBe(true);
    expect(s.stint_plan?.entries.some((e) => e.current)).toBe(true);
  });

  it('has a map outline with enough points', () => {
    const m = demoMap();
    expect(m.points.length).toBeGreaterThan(50);
    expect(m.length).toBe(demoState().track_length);
  });
});
