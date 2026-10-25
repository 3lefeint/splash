import { getMapProgress, getTrackMap } from './api';
import { readJson, safeStorage, writeJson } from './storage';
import type { MapProgress, TrackMap } from './types';

const RETRY_MS = 2_000; // while no outline exists: how often the progress is fetched
const cacheKey = (track: string) => `splash.trackmap.${track}`;

/** The outline of the current track. It is fetched when the track changes and retried until the backend has one. */
class TrackMaps {
  map = $state<TrackMap | null>(null);
  /** The outline recorded so far; set only while the track has no stored map. */
  progress = $state<MapProgress | null>(null);
  private track = '';
  private timer: ReturnType<typeof setTimeout> | null = null;

  watch(track: string | undefined): void {
    if (!track || track === this.track) return;
    this.track = track;
    this.clearTimer();
    this.map = readJson<TrackMap>(safeStorage(), cacheKey(track)); // shown until the server answers, also offline
    void this.load(track);
  }

  /** Forgets the map and looks for a new one, after the stored map was deleted. */
  reload(): void {
    const track = this.track;
    this.track = '';
    this.map = null;
    this.progress = null;
    this.watch(track);
  }

  private async load(track: string): Promise<void> {
    try {
      // the progress comes first: it is cheap and the stored map is looked up only once it is gone
      const p = await getMapProgress(track);
      if (track !== this.track) return;
      this.progress = p;
      if (!p) {
        const m = await getTrackMap(track);
        if (track !== this.track) return;
        if (m) {
          this.map = m;
          this.progress = null;
          writeJson(safeStorage(), cacheKey(track), m);
          return;
        }
        this.map = null;
      }
    } catch {
      // keep what is shown and try again
    }
    if (track === this.track) this.timer = setTimeout(() => void this.load(track), RETRY_MS);
  }

  private clearTimer(): void {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }
}

export const trackMaps = new TrackMaps();
