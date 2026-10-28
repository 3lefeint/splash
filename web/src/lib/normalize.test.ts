import { describe, expect, it } from 'vitest';
import { normalizeState } from './normalize';
import type { State } from './types';

const bare = (over: Record<string, unknown> = {}): State =>
  ({ ready: true, et: 1, time_left: 2, opponents: { ahead: null, behind: null }, params: {}, overrides: {}, ...over }) as unknown as State;

describe('normalizeState', () => {
  it('turns null lists into empty ones, wherever the backend can send them', () => {
    const s = normalizeState(
      bare({
        car: { wear: null },
        stint_plan: { entries: null },
        pits: { rows: null },
        weather: { history: null },
        wear_stats: { recent: null, per_lap: null, per_wheel: null, base: null },
        strategy: { rivals: [{ wear: null }, { wear: [1, 1, 1, 1] }], rejoin: { window: null, passed_by: null, also_stop: null } },
        sections: { ahead: { delta: null }, behind: { delta: [null, 1] }, pits: { boxes: null } },
      }),
    );
    expect(s.car?.wear).toEqual([]);
    expect(s.stint_plan?.entries).toEqual([]);
    expect(s.pits?.rows).toEqual([]);
    expect(s.weather?.history).toEqual([]);
    expect(s.wear_stats).toMatchObject({ recent: [], per_lap: [], per_wheel: [], base: undefined });
    expect(s.strategy?.rivals.map((r) => r.wear)).toEqual([[], [1, 1, 1, 1]]);
    expect(s.strategy?.rejoin).toMatchObject({ window: [], passed_by: [], also_stop: [] });
    expect(s.sections?.ahead?.delta).toEqual([]);
    expect(s.sections?.behind?.delta).toEqual([null, 1]);
    expect(s.sections?.pits?.boxes).toEqual([]);
  });

  it('turns a missing rivals list, opponents and overrides into safe values', () => {
    const raw = { ready: false, et: 0, time_left: 0, strategy: {} } as unknown as State;
    const s = normalizeState(raw);
    expect(s.strategy?.rivals).toEqual([]);
    expect(s.opponents).toEqual({ ahead: null, behind: null });
    expect(s.overrides).toEqual({});
  });

  it('leaves filled data and absent optional parts alone', () => {
    const s = bare({ strategy: { rivals: [{ car_id: 1, wear: [0.9] }] }, field: [{ car_id: 1 }] });
    const before = JSON.stringify(s);
    normalizeState(s);
    expect(s.yellow_sectors).toEqual([]); // the one list every state gets
    delete s.yellow_sectors;
    expect(JSON.stringify(s)).toBe(before);
    expect(normalizeState(bare()).strategy).toBeUndefined();
  });

  it('turns a null field or agents list into absent', () => {
    const s = normalizeState(bare({ field: null, agents: null }));
    expect(s.field).toBeUndefined();
    expect(s.agents).toBeUndefined();
  });
});
