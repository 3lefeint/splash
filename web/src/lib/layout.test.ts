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
