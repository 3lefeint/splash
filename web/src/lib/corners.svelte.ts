import { getCorners, putCorners } from './api';
import type { Corner } from './types';

/** The corner names of the current track, and which end of which corner the next click on the map sets. */
class Corners {
  list = $state<Corner[]>([]);
  /** Index of the corner being marked, or null. */
  selected = $state<number | null>(null);
  /** Which end the next click sets. */
  step = $state<'from' | 'to'>('from');
  error = $state('');
  private current = '';

  /** The track the list belongs to. */
  get track(): string {
    return this.current;
  }

  watch(track: string | undefined): void {
    if (!track || track === this.current) return;
    this.current = track;
    this.list = [];
    this.selected = null;
    void getCorners(track).then(
      (l) => {
        if (track === this.current) this.list = l;
      },
      () => {},
    );
  }

  async save(list: Corner[]): Promise<void> {
    const track = this.current;
    if (!track) return;
    this.list = list;
    try {
      await putCorners(track, list);
      this.error = '';
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
    }
  }

  /** Starts marking a corner: the first click sets where it starts, the second where it ends. */
  mark(i: number): void {
    this.selected = i;
    this.step = 'from';
  }

  stop(): void {
    this.selected = null;
  }

  /** Sets the start or the end of the corner being marked; after the end the next unmarked corner is up. */
  async place(dist: number): Promise<void> {
    const i = this.selected;
    if (i == null || !this.list[i]) return;
    const d = Math.round(dist);
    if (this.step === 'from') {
      this.step = 'to';
      await this.save(this.list.map((c, k) => (k === i ? { ...c, from: d, to: null } : c)));
      return;
    }
    const list = this.list.map((c, k) => (k === i ? { ...c, to: d } : c));
    const next = list.findIndex((c, k) => k > i && (c.from == null || c.to == null));
    if (next >= 0) this.mark(next);
    else this.selected = null;
    await this.save(list);
  }

  setLabel(i: number, label: [number, number] | null): Promise<void> {
    return this.save(this.list.map((c, k) => (k === i ? { ...c, label } : c)));
  }
}

export const corners = new Corners();
