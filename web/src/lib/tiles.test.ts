import { describe, expect, it } from 'vitest';
import { dashboardLayout } from './layout.svelte';
import { cleanOptions } from './tiles';

const known = new Set(['stops', 'setups', 'map']);

describe('tile options', () => {
  it('keeps only valid stored values for known tiles', () => {
    const o = cleanOptions(
      {
        stops: { size: 'l', hide: ['saveDelta', 'nonsense', 3] },
        setups: { columns: 3, grid: [{ id: 'Fuel', x: 0, y: 0, w: 2, h: 1 }, { id: 'bad', x: -1, y: 0, w: 1, h: 1 }] },
        map: { size: 'xl', columns: 9 },
        gone: { size: 's' },
      },
      known,
    );
    expect(o.stops).toEqual({ size: 'l', hide: ['saveDelta'] });
    expect(o.setups).toEqual({ columns: 3, grid: [{ id: 'Fuel', x: 0, y: 0, w: 2, h: 1 }] });
    expect(o.map).toEqual({});
    expect(o.gone).toBeUndefined();
  });

  it('survives garbage', () => {
    expect(cleanOptions(null, known)).toEqual({});
    expect(cleanOptions('x', known)).toEqual({});
  });
});

describe('dashboard layout options', () => {
  it('switches a line off and on and resets with the layout', () => {
    dashboardLayout.reset();
    expect(dashboardLayout.shows('stops', 'saveDelta')).toBe(true);
    dashboardLayout.toggleLine('stops', 'saveDelta');
    expect(dashboardLayout.shows('stops', 'saveDelta')).toBe(false);
    expect(dashboardLayout.shows('stops', 'lapsLeft')).toBe(true);
    dashboardLayout.setOption('stops', { size: 's' });
    expect(dashboardLayout.optionsOf('stops')).toEqual({ hide: ['saveDelta'], size: 's' });
    dashboardLayout.toggleLine('stops', 'saveDelta');
    expect(dashboardLayout.shows('stops', 'saveDelta')).toBe(true);
    dashboardLayout.reset();
    expect(dashboardLayout.optionsOf('stops')).toEqual({});
  });
});
