import { describe, expect, it } from 'vitest';
import { parseRoute, visibleRoutes } from './router.svelte';

describe('router', () => {
  it('shows the admin page in the navigation only to administrators', () => {
    expect(visibleRoutes(false)).not.toContain('admin');
    expect(visibleRoutes(true)).toContain('admin');
    expect(visibleRoutes(false)).toContain('plan');
  });
  it('knows the admin route and falls back to the dashboard', () => {
    expect(parseRoute('#/admin')).toBe('admin');
    expect(parseRoute('#/nope')).toBe('dashboard');
  });
});
