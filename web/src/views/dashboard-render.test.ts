import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import Dashboard from './Dashboard.svelte';
import { dashboardLayout } from '../lib/layout.svelte';
import { router } from '../lib/router.svelte';
import { live } from '../lib/store';

describe('the dashboard renders', () => {
  it('shows the wait message without data', () => {
    live.state = null;
    dashboardLayout.edit = false;
    expect(render(Dashboard).body).toContain('class="wait');
  });

  it('shows the example tiles while arranged without data, in every display mode', () => {
    live.state = null;
    router.route = 'dashboard';
    dashboardLayout.edit = true;
    for (const mode of ['normal', 'compact', 'value'] as const) {
      for (const id of ['next', 'fuel', 'stops', 'driver', 'opponents', 'plan']) dashboardLayout.setOption(id, { mode });
      const out = render(Dashboard).body;
      expect(out).not.toContain('class="wait');
      expect(out).toContain('class="tile');
    }
    dashboardLayout.edit = false;
    dashboardLayout.reset();
  });
});
