import { describe, expect, it } from 'vitest';
import { alerts } from './strategy';
import { announcements } from './announce';
import { normalizeState } from './normalize';
import { migrateYellow, yellowCauses, yellowLine } from './yellow';
import { DEFAULT_RANGES, FALLBACK_RANGE } from './conditions';
import type { Corner, FieldCar, State } from './types';

const car = (id: number, dist: number, over: Partial<FieldCar> = {}): FieldCar =>
  ({ car_id: id, vehicle_name: `c${id}`, driver: `D${id}`, class: 'GT3', place: id + 1, lap_dist: dist, in_pits: false, ours: false, ...over }) as FieldCar;
const cause = (id: number, dist: number, over: Partial<FieldCar> = {}) => car(id, dist, { probable_cause: true, ...over });
const me = (dist: number, over: Partial<FieldCar> = {}) => car(0, dist, { ours: true, ...over });

const state = (over: Record<string, unknown> = {}): State =>
  ({ ready: true, et: 1, time_left: 2, opponents: { ahead: null, behind: null }, params: {}, overrides: {}, track_length: 5000, yellow_sectors: [2], ...over }) as unknown as State;

const corner = (name: string, from: number, to: number): Corner => ({ name, from, to, label: null });
const corners = [corner('Karussell', 1000, 1200), corner('Hatzenbach', 4900, 100), corner('Schwalbenschwanz', 2000, 2100)];

describe('yellowLine', () => {
  it('shows nothing while no sector is flagged', () => {
    expect(yellowLine(state({ yellow_sectors: [], field: [me(0), cause(1, 500)] }), corners)).toBeNull();
    expect(yellowLine(state({ yellow_sectors: null }), corners)).toBeNull();
    expect(yellowLine(null, corners)).toBeNull();
  });
  it('names the distance to a cause ahead and the corner it comes before', () => {
    const line = yellowLine(state({ field: [me(500), cause(1, 840)] }), corners);
    expect(line).toEqual({ kind: 'ahead', dist: 340, corner: { name: 'Karussell', mode: 'before' }, others: 0 });
  });
  it('says "in" when the cause is inside a corner', () => {
    const line = yellowLine(state({ field: [me(500), cause(1, 1100)] }), corners);
    expect(line).toMatchObject({ dist: 600, corner: { name: 'Karussell', mode: 'in' } });
  });
  it('has no corner when none is stored', () => {
    expect(yellowLine(state({ field: [me(500), cause(1, 840)] }), [])).toMatchObject({ kind: 'ahead', dist: 340, corner: null });
    expect(yellowLine(state({ field: [me(500), cause(1, 840)] }), [{ name: 'x', from: null, to: null, label: null }])).toMatchObject({ corner: null });
  });
  it('wraps around the start line, also for the corner that crosses it', () => {
    const line = yellowLine(state({ field: [me(4800), cause(1, 50)] }), corners);
    expect(line).toMatchObject({ kind: 'ahead', dist: 250, corner: { name: 'Hatzenbach', mode: 'in' } });
    expect(yellowLine(state({ field: [me(4800), cause(1, 400)] }), corners)).toMatchObject({ dist: 600, corner: { name: 'Karussell', mode: 'before' } });
  });
  it('takes the nearest cause ahead and counts the others, causes behind do not count', () => {
    const s = state({ field: [me(500), cause(1, 3000), cause(2, 840), cause(3, 2100), cause(4, 400)] });
    expect(yellowLine(s, corners)).toMatchObject({ kind: 'ahead', dist: 340, others: 2 });
  });
  it('leaves out a cause that lies behind the car, so only the map shows it', () => {
    expect(yellowLine(state({ field: [me(500), cause(1, 400)] }), corners)).toBeNull();
    expect(yellowLine(state({ field: [me(500), cause(1, 100)] }), corners)).toBeNull(); // 4600 m ahead is 400 m behind
    expect(yellowLine(state({ field: [me(500), cause(1, 3000)] }), corners)).toMatchObject({ dist: 2500 }); // exactly half a lap
  });
  it('says the cause is unknown when a sector is flagged and no car qualifies', () => {
    expect(yellowLine(state({ field: [me(500), car(1, 840)] }), corners)).toEqual({ kind: 'unknown' });
    expect(yellowLine(state({ field: [me(500), cause(1, 840, { in_pits: true })] }), corners)).toEqual({ kind: 'unknown' });
    expect(yellowLine(state({ field: [me(500, { probable_cause: true })] }), corners)).toEqual({ kind: 'unknown' });
    expect(yellowLine(state({ field: undefined }), corners)).toEqual({ kind: 'unknown' });
  });
  it('shows nothing while our car is in the pits', () => {
    expect(yellowLine(state({ field: [me(500, { in_pits: true }), cause(1, 840)] }), corners)).toBeNull();
    expect(yellowLine(state({ field: [me(500, { in_pits: true }), car(1, 840)] }), corners)).toBeNull();
  });
  it('does not guess without our car or the track length', () => {
    expect(yellowLine(state({ field: [cause(1, 840)] }), corners)).toBeNull();
    expect(yellowLine(state({ field: [me(500), cause(1, 840)], track_length: 0 }), corners)).toBeNull();
  });
  it('lists the causes without our car and without cars in the pits', () => {
    const s = state({ field: [me(1, { probable_cause: true }), cause(1, 2), cause(2, 3, { in_pits: true }), car(3, 4)] });
    expect(yellowCauses(s).map((c) => c.car_id)).toEqual([1]);
    expect(yellowCauses(null)).toEqual([]);
  });
});

const ids = (s: State | null, yellow: boolean) => alerts(s, DEFAULT_RANGES, FALLBACK_RANGE, { yellow, corners }).map((a) => a.id);

describe('the yellow alerts', () => {
  it('shows the line with the nearest cause, the count of the others small, only with the setting on', () => {
    const s = state({ field: [me(500), cause(1, 840), cause(2, 2000)] });
    expect(ids(s, true)).toContain('yellow-ahead');
    expect(ids(s, false)).not.toContain('yellow-ahead');
    const a = alerts(s, DEFAULT_RANGES, FALLBACK_RANGE, { yellow: true, corners }).find((x) => x.id === 'yellow-ahead')!;
    expect(a).toMatchObject({ level: 'warn', key: 'alert.yellowAheadBefore', params: { dist: 340, corner: 'Karussell' }, small: { key: 'alert.yellowOthers', params: { n: 1 } } });
    expect(announcements(s, 0, alerts(s, DEFAULT_RANGES, FALLBACK_RANGE, { yellow: true, corners })).map((x) => x.id)).toContain('yellow-ahead');
  });
  it('picks the text by corner: before, in, none', () => {
    const key = (field: FieldCar[], c: Corner[]) => alerts(state({ field }), DEFAULT_RANGES, FALLBACK_RANGE, { yellow: true, corners: c }).find((x) => x.id === 'yellow-ahead')?.key;
    expect(key([me(500), cause(1, 840)], corners)).toBe('alert.yellowAheadBefore');
    expect(key([me(500), cause(1, 1100)], corners)).toBe('alert.yellowAheadIn');
    expect(key([me(500), cause(1, 840)], [])).toBe('alert.yellowAhead');
  });
  it('shows a neutral note without a sound when the cause is unknown', () => {
    const s = state({ field: [me(500), car(1, 840)] });
    const list = alerts(s, DEFAULT_RANGES, FALLBACK_RANGE, { yellow: true, corners });
    expect(list.find((x) => x.id === 'yellow-unknown')).toMatchObject({ level: 'info', key: 'alert.yellowUnknown' });
    expect(announcements(s, 0, list).map((x) => x.id)).not.toContain('yellow-unknown');
  });
  it('shows a full course yellow alert independent of the sectors and the pit parameters', () => {
    const s = state({ fcy: true, yellow_sectors: [] });
    expect(ids(s, true)).toContain('yellow-fcy');
    expect(ids(s, false)).not.toContain('yellow-fcy');
    expect(announcements(s, 0, alerts(s, DEFAULT_RANGES, FALLBACK_RANGE, { yellow: true })).map((x) => x.id)).toContain('yellow-fcy');
  });
  it('shows nothing about the own sector any more', () => {
    const s = state({ field: [me(500)], yellow_sectors: [1] });
    expect(ids(s, true).filter((i) => i.startsWith('yellow') && i !== 'yellow-unknown')).toEqual([]);
  });
  it('shows nothing while all sectors are clear, and survives missing or null fields', () => {
    expect(ids(state({ yellow_sectors: [] }), true)).toEqual([]);
    expect(ids(state({ yellow_sectors: undefined }), true)).toEqual([]);
    expect(ids(null, true)).toEqual([]);
    expect(ids(normalizeState(state({ yellow_sectors: null, field: null })), true)).toEqual([]);
  });
});

describe('the yellow settings', () => {
  it('keep the choice saved under the old names', () => {
    expect(migrateYellow({ yellowAlert: false, yellowMap: true })).toEqual({ yellowAhead: false, yellowMarkers: true });
    expect(migrateYellow({ yellowAlert: true, yellowMap: false })).toEqual({ yellowAhead: true, yellowMarkers: false });
  });
  it('prefer the new names and default to on', () => {
    expect(migrateYellow({ yellowAhead: true, yellowMarkers: false, yellowAlert: false, yellowMap: true })).toEqual({ yellowAhead: true, yellowMarkers: false });
    expect(migrateYellow({})).toEqual({ yellowAhead: true, yellowMarkers: true });
    expect(migrateYellow(null)).toEqual({ yellowAhead: true, yellowMarkers: true });
  });
});
