import { compact, reconcile, rows, type Item } from './grid';
import { readJson, removeKey, safeStorage, writeJson } from './storage';
import { cleanOptions, OFF_BY_DEFAULT, type TileOptions } from './tiles';

const KEY = 'splash.layout.dashboard';
const VERSION = 2; // 1 is the same without the options of the tiles

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

/** What an administrator saves as the default of the team: the same three parts the browser stores. */
export interface LayoutDoc {
  items: Item[];
  hidden: string[];
  options: Record<string, TileOptions>;
}

const KNOWN = new Set(DEFAULT_DASHBOARD.map((d) => d.id));

/** Takes a layout as stored or received and keeps what is valid; tiles that are missing come back at their default place. */
function cleanDoc(raw: unknown): LayoutDoc | null {
  if (!raw || typeof raw !== 'object') return null;
  const r = raw as { items?: unknown; hidden?: unknown; options?: unknown };
  return {
    items: reconcile(r.items, DEFAULT_DASHBOARD),
    hidden: Array.isArray(r.hidden) ? r.hidden.filter((id): id is string => typeof id === 'string' && KNOWN.has(id)) : [],
    options: cleanOptions(r.options, KNOWN),
  };
}

class DashboardLayout {
  /** Positions of all tiles, hidden ones included, so a tile comes back where it was. */
  items = $state<Item[]>(DEFAULT_DASHBOARD);
  hidden = $state<string[]>([]);
  /** Text size, switched off lines and the like per tile. */
  options = $state<Record<string, TileOptions>>({});
  edit = $state(false);
  /** Show the example values while arranging even though an agent sends data; not stored. */
  demo = $state(false);

  get visible(): Item[] {
    return this.items.filter((i) => !this.hidden.includes(i.id));
  }

  /** True once this browser stores a layout of its own; until then the team default (or the built-in one) applies. */
  private own = false;
  /** The default the administrator saved for the team, when the server has one. */
  private team: LayoutDoc | null = null;

  constructor() {
    const saved = readJson<{ v: number; items: unknown; hidden?: unknown; options?: unknown }>(safeStorage(), KEY);
    const ok = saved?.v === 1 || saved?.v === VERSION;
    this.own = ok;
    this.adopt(ok ? cleanDoc(saved) : null);
  }

  private adopt(doc: LayoutDoc | null): void {
    this.items = doc ? doc.items.map((i) => ({ ...i })) : reconcile(null, DEFAULT_DASHBOARD);
    this.hidden = doc ? [...doc.hidden] : [];
    this.options = doc ? structuredClone(doc.options) : {};
  }

  private save(): void {
    this.own = true;
    writeJson(safeStorage(), KEY, { v: VERSION, items: this.items, hidden: this.hidden, options: this.options });
  }

  /** Takes the default the server holds for the team; a browser with a layout of its own keeps that one. */
  setTeamDefault(raw: unknown): void {
    this.team = cleanDoc(raw);
    if (!this.own) this.adopt(this.team);
  }

  /** The layout as it is now, for saving it as the default of the team. */
  snapshot(): LayoutDoc {
    return { items: this.items.map((i) => ({ ...i })), hidden: [...this.hidden], options: $state.snapshot(this.options) };
  }

  optionsOf(id: string): TileOptions {
    return this.options[id] ?? {};
  }

  /** False when the line is switched off in the gear of the tile (or off by default and not switched on). */
  shows(id: string, line: string): boolean {
    const o = this.optionsOf(id);
    return OFF_BY_DEFAULT[id]?.includes(line) ? !!o.show?.includes(line) : !o.hide?.includes(line);
  }

  setOption(id: string, patch: Partial<TileOptions>): void {
    this.options = { ...this.options, [id]: { ...this.optionsOf(id), ...patch } };
    this.save();
  }

  toggleLine(id: string, line: string): void {
    const o = this.optionsOf(id);
    const key = OFF_BY_DEFAULT[id]?.includes(line) ? 'show' : 'hide';
    const list = o[key] ?? [];
    this.setOption(id, { [key]: list.includes(line) ? list.filter((l) => l !== line) : [...list, line] });
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

  /** Back to the default of the team, or the built-in one; the browser stores nothing of its own afterwards. */
  reset(): void {
    this.own = false;
    removeKey(safeStorage(), KEY);
    this.adopt(this.team);
  }
}

export const dashboardLayout = new DashboardLayout();
