import { describe, expect, it } from 'vitest';
import { Animator, carNumber, classColor, fitView, hideKey, pointAt, wrapDelta } from './trackmap';
import type { FieldCar } from './types';

const square = { length: 400, points: [[0, 0], [100, 0], [100, 100], [0, 100]] as [number, number][] };
const car = (dist: number, extra: Partial<FieldCar> = {}): FieldCar => ({
  car_id: 1, vehicle_name: 'a #7', driver: '', class: 'GT3', place: 1, lap_dist: dist, in_pits: false, ours: false, ...extra,
});

describe('pointAt', () => {
  it('interpolates between points and wraps around the lap', () => {
    expect(pointAt(square, 0)).toEqual([0, 0]);
    expect(pointAt(square, 50)).toEqual([50, 0]);
    expect(pointAt(square, 350)).toEqual([0, 50]);
    expect(pointAt(square, 400 + 50)).toEqual([50, 0]);
    expect(pointAt(square, -50)).toEqual([0, 50]);
  });
});

describe('fitView', () => {
  it('flips z so north is up and keeps a margin', () => {
    const v = fitView(square.points, 0);
    expect(v.project([10, 20])).toEqual([10, -20]);
    expect(v.viewBox).toBe('0 -100 100 100');
    expect(v.path.startsWith('M0 0')).toBe(true);
    expect(v.path.endsWith('Z')).toBe(true);
  });
});

describe('wrapDelta', () => {
  it('takes the short way across the start line', () => {
    expect(wrapDelta(1990, 10, 2000)).toBe(20);
    expect(wrapDelta(10, 1990, 2000)).toBe(-20);
    expect(wrapDelta(100, 150, 2000)).toBe(50);
  });
});

describe('Animator', () => {
  it('continues with the last speed and stops extrapolating after the limit', () => {
    const a = new Animator(2000);
    a.update([car(100)], 0);
    a.update([car(150)], 1000); // 50 m/s
    expect(a.at(car(150), 1000)).toBe(150);
    expect(a.at(car(150), 1500)).toBeCloseTo(175);
    expect(a.at(car(150), 9000)).toBeCloseTo(150 + 50 * 1.5);
  });

  it('wraps across the start line and ignores cars that went backwards', () => {
    const a = new Animator(2000);
    a.update([car(1980), car(500, { car_id: 2 })], 0);
    a.update([car(20), car(480, { car_id: 2 })], 1000);
    expect(a.at(car(20), 1500)).toBeCloseTo(40);
    expect(a.at(car(480, { car_id: 2 }), 1500)).toBe(480);
  });

  it('forgets cars that left the field', () => {
    const a = new Animator(2000);
    a.update([car(100)], 0);
    a.update([], 1000);
    expect(a.at(car(100), 1000)).toBeNull();
  });
});

describe('helpers', () => {
  it('colours classes stably by name', () => {
    expect(classColor(['LMP2', 'GT3'], 'GT3')).toBe(classColor(['GT3', 'LMP2', 'GT3'], 'GT3'));
    expect(classColor(['GT3', 'LMP2'], 'GT3')).not.toBe(classColor(['GT3', 'LMP2'], 'LMP2'));
  });
  it('keeps cars with the same name apart when hiding', () => {
    expect(hideKey(car(1, { car_id: 3 }))).not.toBe(hideKey(car(1, { car_id: 4 })));
  });
  it('reads the start number', () => {
    expect(carNumber('BMW M4 GT3 #06')).toBe('06');
    expect(carNumber('No number')).toBe('');
  });
});
