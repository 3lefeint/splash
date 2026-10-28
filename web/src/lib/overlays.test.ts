import { describe, expect, it } from 'vitest';
import { paintFor, targetName } from './overlays';
import type { State } from './types';

const sec = (extra: object) => ({ sections: { segments: 3, laps: 3, ...extra } }) as unknown as State;

describe('paintFor', () => {
  it('is empty without data or when the overlay is off', () => {
    expect(paintFor(null, 'time', 'all', 'ahead')).toBeNull();
    expect(paintFor(sec({}), 'off', 'all', 'ahead')).toBeNull();
    expect(paintFor(sec({}), 'time', 'all', 'ahead')).toBeNull();
    expect(paintFor(sec({ fuel: [null, null] }), 'fuel', 'all', 'ahead')).toBeNull();
  });

  it('paints the time against the chosen car, green below zero and red above', () => {
    const s = sec({ ahead: { name: 'A', laps: 3, delta: [-0.2, null, 0.4] }, behind: { name: 'B', laps: 3, delta: [0.1, 0.1, 0.1] }, best: [0, 0.05, 0.3] });
    expect(paintFor(s, 'time', 'all', 'ahead')).toEqual({ values: [-0.2, null, 0.4], max: 0.4, mode: 'diverging' });
    expect(paintFor(s, 'time', 'all', 'behind')?.values).toEqual([0.1, 0.1, 0.1]);
    expect(paintFor(s, 'time', 'all', 'best')?.max).toBeCloseTo(0.3);
    expect(targetName(s, 'ahead')).toBe('A');
    expect(targetName(s, 'best')).toBe('');
  });

  it('paints fuel and braking directly and speed upside down', () => {
    const s = sec({ fuel: [0.1, 0.3, null], brake: [0, 0.9, 0.2], speed: [50, 20, 40] });
    const fuel = paintFor(s, 'fuel', 'all', 'ahead');
    expect(fuel?.values[0]).toBeCloseTo(0); // the section with the lowest use is the quiet end
    expect(fuel?.values[1]).toBeCloseTo(0.2);
    expect(fuel?.values[2]).toBeNull();
    expect(fuel?.max).toBeCloseTo(0.2);
    expect(paintFor(s, 'brake', 'all', 'ahead')?.max).toBe(1);
    const speed = paintFor(s, 'speed', 'all', 'ahead');
    expect(speed?.values[0]).toBeCloseTo(0); // the fastest section is quiet
    expect(speed?.values[1]).toBeCloseTo((50 - 20) * 3.6); // the slowest is strong
  });
});
