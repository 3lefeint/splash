export const routes = ['dashboard', 'plan', 'map', 'sessions', 'settings', 'tracks', 'rest', 'admin'] as const;
export type Route = (typeof routes)[number];

/** The pages shown in the navigation: the admin page only for administrators. */
export const visibleRoutes = (admin: boolean): Route[] => routes.filter((r) => r !== 'admin' || admin);

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
