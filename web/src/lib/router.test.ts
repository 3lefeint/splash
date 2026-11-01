import { describe, expect, it } from 'vitest';
import { parseRoute, visibleRoutes } from './router.svelte';

describe('router', () => {
  it('shows the administration page to every member, because it holds their own agent', () => {
    expect(visibleRoutes()).toContain('admin');
    expect(visibleRoutes()).toContain('plan');
  });
  it('knows the admin route and falls back to the dashboard', () => {
    expect(parseRoute('#/admin')).toBe('admin');
    expect(parseRoute('#/nope')).toBe('dashboard');
  });
});
