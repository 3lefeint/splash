import { getContext, setContext } from 'svelte';

const KEY = Symbol('tile-info');

/** The explanatory texts of the content of one tile; the tile shows them behind its "i" button. */
export class TileInfo {
  private texts = $state<Record<number, string | null>>({});
  private next = 0;

  reserve(): number {
    return this.next++;
  }

  set(id: number, text: string | null): void {
    this.texts[id] = text;
  }

  get list(): string[] {
    return Object.keys(this.texts)
      .map(Number)
      .sort((a, b) => a - b)
      .map((k) => this.texts[k])
      .filter((x): x is string => !!x);
  }
}

/** Called by a tile: lets its content hand in texts. */
export function provideTileInfo(): TileInfo {
  const info = new TileInfo();
  setContext(KEY, info);
  return info;
}

/**
 * Called by content in a tile: ``text`` is shown behind the "i" of the tile while it returns a string.
 * Returns false outside a tile, where the caller should show the text itself.
 */
export function tileInfo(text: () => string | null): boolean {
  const info = getContext<TileInfo | undefined>(KEY);
  if (!info) return false;
  const id = info.reserve();
  $effect(() => {
    info.set(id, text());
    return () => info.set(id, null);
  });
  return true;
}
