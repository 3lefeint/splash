import { describe, expect, it } from 'vitest';
import { dashboardLayout, DEFAULT_DASHBOARD } from './layout.svelte';

describe('dashboard layout', () => {
  it('hides a tile and closes the gap, and shows it again at its old size', () => {
    dashboardLayout.reset();
    const tyres = DEFAULT_DASHBOARD.find((i) => i.id === 'tyres')!;
    dashboardLayout.hide('tyres');
    expect(dashboardLayout.visible.some((i) => i.id === 'tyres')).toBe(false);
    expect(dashboardLayout.hidden).toEqual(['tyres']);
    dashboardLayout.show('tyres');
    const back = dashboardLayout.visible.find((i) => i.id === 'tyres')!;
    expect([back.w, back.h]).toEqual([tyres.w, tyres.h]);
    expect(dashboardLayout.hidden).toEqual([]);
  });

  it('keeps the position of hidden tiles when the others move', () => {
    dashboardLayout.reset();
    dashboardLayout.hide('map');
    dashboardLayout.set(dashboardLayout.visible.map((i) => ({ ...i })));
    expect(dashboardLayout.items.some((i) => i.id === 'map')).toBe(true);
  });

  it('reset shows everything', () => {
    dashboardLayout.hide('fuel');
    dashboardLayout.reset();
    expect(dashboardLayout.hidden).toEqual([]);
    expect(dashboardLayout.visible).toHaveLength(DEFAULT_DASHBOARD.length);
  });
});

describe('team default of the dashboard layout', () => {
  it('applies until the browser changes something of its own, and reset returns to it', () => {
    dashboardLayout.reset();
    dashboardLayout.setTeamDefault({ items: [{ id: 'fuel', x: 0, y: 0, w: 6, h: 2 }], hidden: ['map', 'nope'], options: { fuel: { mode: 'compact' } } });
    expect(dashboardLayout.hidden).toEqual(['map']);
    expect(dashboardLayout.items.find((i) => i.id === 'fuel')).toMatchObject({ w: 6, h: 2 });
    expect(dashboardLayout.optionsOf('fuel').mode).toBe('compact');
    expect(dashboardLayout.items).toHaveLength(DEFAULT_DASHBOARD.length); // tiles the default lacks are added

    dashboardLayout.hide('tyres');
    dashboardLayout.setTeamDefault({ hidden: [] }); // a later change of the default does not touch an own layout
    expect(dashboardLayout.hidden).toContain('tyres');

    dashboardLayout.reset();
    expect(dashboardLayout.hidden).toEqual([]);
    dashboardLayout.setTeamDefault({ hidden: ['map'] }); // not own again after the reset: follows the default
    expect(dashboardLayout.hidden).toEqual(['map']);

    dashboardLayout.setTeamDefault(undefined);
    dashboardLayout.reset();
    expect(dashboardLayout.hidden).toEqual([]);
  });

  it('snapshot gives a copy that later changes do not reach', () => {
    dashboardLayout.reset();
    const snap = dashboardLayout.snapshot();
    dashboardLayout.hide('fuel');
    expect(snap.hidden).toEqual([]);
  });
});
