export const routes = ['dashboard', 'plan', 'map', 'sessions', 'settings', 'tracks', 'rest', 'admin'] as const;
export type Route = (typeof routes)[number];

/** The pages shown in the navigation. The administration page is for every member: it holds their own agent, and the
 * rest of it appears only for administrators. */
export const visibleRoutes = (): Route[] => [...routes];

export function parseRoute(hash: string): Route {
  const name = hash.replace(/^#\/?/, '');
  return (routes as readonly string[]).includes(name) ? (name as Route) : 'dashboard';
}

class Router {
  route = $state<Route>('dashboard');

  start(): void {
    const update = () => (this.route = parseRoute(location.hash));
    update();
    addEventListener('hashchange', update);
  }
}

export const router = new Router();
