import { demoMap, demoState } from './demo';
import { dashboardLayout } from './layout.svelte';
import { router } from './router.svelte';
import { live } from './store';
import type { State, TrackMap } from './types';

const DEMO = demoState();
const DEMO_MAP = demoMap();

/**
 * What the tiles of the dashboard show. Normally the live state; while the dashboard is being arranged and no agent has
 * sent data, an invented example, so the tiles can be laid out without the game running. The example is never used
 * anywhere else: the announcements and the title in the header read the live state itself.
 */
class View {
  get demo(): boolean {
    return router.route === 'dashboard' && dashboardLayout.edit && !(live.state?.ready && live.state.car);
  }

  get state(): State | null {
    return this.demo ? DEMO : live.state;
  }

  get map(): TrackMap | null {
    return this.demo ? DEMO_MAP : null;
  }

  get etNow(): number {
    return this.demo ? DEMO.et : live.etNow;
  }

  get nowMs(): number {
    return live.nowMs;
  }
}

export const view = new View();
