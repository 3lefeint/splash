import { describe, expect, it } from 'vitest';
import { compact, place, reconcile, rows, type Item } from './grid';

const it_ = (id: string, x: number, y: number, w: number, h: number): Item => ({ id, x, y, w, h });
const pos = (items: Item[], id: string) => {
  const i = items.find((v) => v.id === id)!;
  return [i.x, i.y, i.w, i.h];
};

describe('compact', () => {
  it('moves items up into free space but not through others', () => {
    const out = compact([it_('a', 0, 0, 6, 2), it_('b', 0, 5, 6, 2), it_('c', 6, 4, 6, 1)]);
    expect(pos(out, 'b')).toEqual([0, 2, 6, 2]);
    expect(pos(out, 'c')).toEqual([6, 0, 6, 1]);
  });
});

describe('place', () => {
  const base = [it_('a', 0, 0, 6, 2), it_('b', 6, 0, 6, 2), it_('c', 0, 2, 12, 2)];

  it('makes the items below yield to a moved item', () => {
    const out = place(base, 'a', { x: 0, y: 2 });
    expect(pos(out, 'a')).toEqual([0, 2, 6, 2]);
    expect(pos(out, 'c')).toEqual([0, 4, 12, 2]);
    expect(pos(out, 'b')).toEqual([6, 0, 6, 2]);
  });

  it('keeps the moved item inside the columns and the others apart', () => {
    const out = place(base, 'b', { x: 99 });
    expect(pos(out, 'b')).toEqual([6, 0, 6, 2]);
    for (const x of out) for (const y of out) {
      if (x.id !== y.id) expect(x.x < y.x + y.w && y.x < x.x + x.w && x.y < y.y + y.h && y.y < x.y + x.h).toBe(false);
    }
  });

  it('resizes and pushes neighbours down', () => {
    const out = place(base, 'a', { w: 12, h: 3 });
    expect(pos(out, 'a')).toEqual([0, 0, 12, 3]);
    expect(pos(out, 'b')[1]).toBe(3);
    expect(pos(out, 'c')[1]).toBe(5);
  });

  it('ignores unknown ids and keeps the item order', () => {
    expect(place(base, 'zzz', { x: 1 })).toBe(base);
    expect(place(base, 'b', { y: 5 }).map((i) => i.id)).toEqual(['a', 'b', 'c']);
  });
});

describe('reconcile', () => {
  const defaults = [it_('a', 0, 0, 6, 2), it_('b', 6, 0, 6, 2)];
  it('uses the stored positions and falls back to the defaults', () => {
    const out = reconcile([{ id: 'a', x: 6, y: 0, w: 6, h: 3 }, { id: 'gone', x: 0, y: 0, w: 1, h: 1 }], defaults);
    expect(pos(out, 'a')).toEqual([6, 0, 6, 3]);
    expect(out.map((i) => i.id)).toEqual(['a', 'b']);
    expect(pos(out, 'b')).toEqual([6, 3, 6, 2]); // its default place is taken now
  });
  it('rejects bad data', () => {
    expect(reconcile('nope', defaults)).toEqual(compact(defaults));
    expect(pos(reconcile([{ id: 'a', x: 10, y: 0, w: 6, h: 2 }], defaults), 'a')).toEqual([0, 0, 6, 2]);
    expect(pos(reconcile([{ id: 'a', x: 0, y: 0, w: 0, h: 2 }], defaults), 'a')).toEqual([0, 0, 6, 2]);
  });
});

describe('rows', () => {
  it('counts the used rows', () => {
    expect(rows([it_('a', 0, 0, 1, 2), it_('b', 1, 3, 1, 4)])).toBe(7);
    expect(rows([])).toBe(0);
  });
});
