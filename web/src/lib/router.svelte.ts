export const routes = ['dashboard', 'plan', 'map', 'sessions', 'settings', 'tracks', 'rest'] as const;
export type Route = (typeof routes)[number];

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
