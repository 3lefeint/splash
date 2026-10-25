import { describe, expect, it } from 'vitest';
import { byPosition, distAt, indices, leader, inside, locate, marked, parseNames, stretches } from './corners';
import { fitView } from './trackmap';
import type { Corner } from './types';

const c = (name: string, from: number | null, to: number | null): Corner => ({ name, from, to, label: null });
const ring = [c('C', 900, 950), c('A', 100, 200), c('B', 500, 600), c('X', 300, null)];

describe('marked', () => {
  it('sorts by start and drops corners with a missing end', () => {
    expect(marked(ring).map((m) => m.name)).toEqual(['A', 'B', 'C']);
  });
});

describe('inside', () => {
  it('handles a stretch across the start line', () => {
    const m = { from: 950, to: 30 };
    expect(inside(m, 980, 1000)).toBe(true);
    expect(inside(m, 10, 1000)).toBe(true);
    expect(inside(m, 500, 1000)).toBe(false);
  });
});

describe('locate', () => {
  it('knows the corner the car is in and the next one', () => {
    const w = locate(ring, 150, 1000);
    expect([w.current?.name, w.next?.name, w.toNext]).toEqual(['A', 'B', 350]);
  });

  it('has no current corner between two of them', () => {
    const w = locate(ring, 700, 1000);
    expect([w.current, w.next?.name, w.toNext]).toEqual([null, 'C', 200]);
  });

  it('wraps to the first corner after the last', () => {
    const w = locate(ring, 970, 1000);
    expect([w.current, w.next?.name, w.toNext]).toEqual([null, 'A', 130]);
  });

  it('copes with nothing marked', () => {
    expect(locate([c('X', 1, null)], 10, 1000).next).toBeNull();
  });
});

describe('distAt', () => {
  it('returns the distance of the nearest outline point', () => {
    const pts: [number, number][] = [[0, 0], [10, 0], [10, 10], [0, 10]];
    expect(distAt(pts, 400, [9, 9])).toBe(200);
  });
});

describe('indices', () => {
  it('lists the points of a stretch, across the seam too', () => {
    expect(indices(10, 1000, 200, 400)).toEqual([2, 3, 4]);
    expect(indices(10, 1000, 800, 100)).toEqual([8, 9, 0, 1]);
  });
});

describe('parseNames', () => {
  it('adds new names in order and skips blanks and repeats', () => {
    const out = parseNames('Hatzenbach\n\n flugplatz \nHATZENBACH\nA', [c('a', 5, 9)]);
    expect(out.map((o) => o.name)).toEqual(['Hatzenbach', 'flugplatz']);
    expect(out[0]).toEqual({ name: 'Hatzenbach', from: null, to: null, label: null });
  });
});

describe('stretches', () => {
  const pts: [number, number][] = [[0, 0], [10, 0], [10, 10], [0, 10]];
  const map = { length: 400, points: pts };
  const view = fitView(pts);

  it('gives a path for a marked stretch and only the start while the end is missing', () => {
    const out = stretches(map, view, [c('A', 100, 200), c('B', 300, null), c('C', null, null)]);
    expect(out.map((o) => o.i)).toEqual([0, 1]);
    expect(out[0].d.startsWith('M')).toBe(true);
    expect(out[1].d).toBe('');
  });
});

describe('leader', () => {
  const pts: [number, number][] = [[0, 0], [10, 0], [20, 0]];

  it('docks at the point of the stretch and the edge of the name nearest to each other', () => {
    // a name of 10 characters at size 10 is about 62 wide: from x 30 to 92, above the stretch
    const l = leader(pts, '0123456789', 30, -20, 10);
    expect([l.x1, l.y1]).toEqual([20, 0]);
    expect(l.x2).toBe(30);
    expect(l.y2).toBe(-17.5); // the lower edge of the name, which is the side facing the stretch
  });

  it('ends above the stretch point when the name spans it', () => {
    const l = leader(pts, 'abcdefghij', 10, -20, 10);
    expect(l.x1).toBe(20);
    expect(l.x2).toBe(20);
  });
});

describe('byPosition', () => {
  it('orders by start, unmarked corners last in their own order, and keeps the list index', () => {
    const out = byPosition([c('X', null, null), c('C', 900, 950), c('A', 100, 200), c('Y', null, null), c('B', 500, null)]);
    expect(out.map((o) => o.corner.name)).toEqual(['A', 'B', 'C', 'X', 'Y']);
    expect(out.map((o) => o.i)).toEqual([2, 4, 1, 0, 3]);
  });
});
