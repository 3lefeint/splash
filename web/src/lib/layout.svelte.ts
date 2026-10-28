import { compact, reconcile, rows, type Item } from './grid';
import { readJson, safeStorage, writeJson } from './storage';

const KEY = 'splash.layout.dashboard';
const VERSION = 1;

/** The overview tiles on a 12 column grid; positions and sizes are in cells. */
export const DEFAULT_DASHBOARD: Item[] = [
  { id: 'next', x: 0, y: 0, w: 4, h: 3 },
  { id: 'fuel', x: 4, y: 0, w: 4, h: 3 },
  { id: 'stops', x: 8, y: 0, w: 4, h: 3 },
  { id: 'driver', x: 0, y: 3, w: 4, h: 3 },
  { id: 'opponents', x: 4, y: 3, w: 4, h: 3 },
  { id: 'plan', x: 8, y: 3, w: 4, h: 3 },
  { id: 'map', x: 0, y: 6, w: 8, h: 5 },
  { id: 'tyres', x: 8, y: 6, w: 4, h: 4 },
  { id: 'weather', x: 8, y: 10, w: 4, h: 3 },
  { id: 'pits', x: 0, y: 11, w: 8, h: 3 },
  { id: 'setups', x: 8, y: 13, w: 4, h: 3 },
  { id: 'rivals', x: 0, y: 14, w: 8, h: 5 },
  { id: 'alerts', x: 8, y: 16, w: 4, h: 3 },
  { id: 'model', x: 0, y: 19, w: 4, h: 3 },
  { id: 'agents', x: 4, y: 19, w: 4, h: 3 },
  { id: 'corner', x: 8, y: 19, w: 4, h: 3 },
];

class DashboardLayout {
  /** Positions of all tiles, hidden ones included, so a tile comes back where it was. */
  items = $state<Item[]>(DEFAULT_DASHBOARD);
  hidden = $state<string[]>([]);
  edit = $state(false);

  get visible(): Item[] {
    return this.items.filter((i) => !this.hidden.includes(i.id));
  }

  constructor() {
    const saved = readJson<{ v: number; items: unknown; hidden?: unknown }>(safeStorage(), KEY);
    this.items = reconcile(saved?.v === VERSION ? saved.items : null, DEFAULT_DASHBOARD);
    const known = new Set(DEFAULT_DASHBOARD.map((d) => d.id));
    if (saved?.v === VERSION && Array.isArray(saved.hidden)) {
      this.hidden = saved.hidden.filter((id): id is string => typeof id === 'string' && known.has(id));
    }
  }

  private save(): void {
    writeJson(safeStorage(), KEY, { v: VERSION, items: this.items, hidden: this.hidden });
  }

  /** Takes the new positions of the visible tiles; hidden tiles keep theirs. */
  set(visible: Item[]): void {
    const moved = new Map(visible.map((i) => [i.id, i]));
    this.items = this.items.map((i) => moved.get(i.id) ?? i);
    this.save();
  }

  hide(id: string): void {
    if (this.hidden.includes(id) || !this.items.some((i) => i.id === id)) return;
    this.hidden = [...this.hidden, id];
    this.settle();
    this.save();
  }

  /** Shows the tile again below the others, at its old size. */
  show(id: string): void {
    const old = this.items.find((i) => i.id === id);
    if (!old || !this.hidden.includes(id)) return;
    const below = rows(this.visible);
    this.hidden = this.hidden.filter((h) => h !== id);
    this.items = this.items.map((i) => (i.id === id ? { ...i, y: below } : i));
    this.settle();
    this.save();
  }

  /** Closes the gaps among the visible tiles. */
  private settle(): void {
    const gone = this.items.filter((i) => this.hidden.includes(i.id));
    this.items = [...compact(this.visible), ...gone];
  }

  reset(): void {
    this.hidden = [];
    this.items = reconcile(null, DEFAULT_DASHBOARD);
    this.save();
  }
}

export const dashboardLayout = new DashboardLayout();
