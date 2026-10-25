import { describe, expect, it } from 'vitest';
import { clockAt, duration, fixed, lapTime, signed } from './format';

describe('duration', () => {
  it('formats minutes and hours', () => {
    expect(duration(125)).toBe('2:05');
    expect(duration(3725)).toBe('1:02:05');
    expect(duration(0)).toBe('0:00');
  });
  it('marks negative values and handles missing ones', () => {
    expect(duration(-65)).toBe('−1:05');
    expect(duration(null)).toBe('–');
    expect(duration(NaN)).toBe('–');
  });
});

describe('numbers', () => {
  it('fixed and signed', () => {
    expect(fixed(2.345, 2)).toBe('2.35');
    expect(fixed(null)).toBe('–');
    expect(signed(1.24)).toBe('+1.2');
    expect(signed(-0.44)).toBe('−0.4');
    expect(signed(0)).toBe('0.0');
  });
  it('lap time', () => {
    expect(lapTime(98.412)).toBe('1:38.412');
    expect(lapTime(61.5)).toBe('1:01.500');
    expect(lapTime(null)).toBe('–');
    expect(lapTime(-1)).toBe('–');
  });
});

describe('clockAt', () => {
  it('shifts the wall clock by the game time difference', () => {
    const now = new Date(2026, 9, 25, 14, 30, 0).getTime();
    expect(clockAt(1000 + 3600, 1000, now)).toBe('15:30');
    expect(clockAt(1000 - 600, 1000, now)).toBe('14:20');
  });
});
