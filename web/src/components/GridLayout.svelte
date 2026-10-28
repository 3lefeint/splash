<script lang="ts">
  import type { Snippet } from 'svelte';
  import { COLS, place, rows, type Item } from '../lib/grid';

  let {
    items,
    edit = false,
    cell,
    onchange,
    onremove,
    removeLabel = '',
    rowHeight = 92,
    gap = 12,
    minWidth = 640,
  }: {
    items: Item[];
    edit?: boolean;
    cell: Snippet<[string, boolean]>;
    onchange: (items: Item[]) => void;
    /** Edit mode shows a button on each tile that calls this. */
    onremove?: (id: string) => void;
    removeLabel?: string;
    rowHeight?: number;
    gap?: number;
    /** Below this width the tiles are stacked in one column and cannot be moved. */
    minWidth?: number;
  } = $props();

  let width = $state(0);
  let drag = $state<{ id: string; mode: 'move' | 'size'; x: number; y: number; start: Item[]; live: Item[] } | null>(null);

  const narrow = $derived(width > 0 && width < minWidth);
  const shown = $derived(drag ? drag.live : items);
  const colW = $derived((width + gap) / COLS);
  const rowH = $derived(rowHeight + gap);
  const height = $derived(Math.max(rows(shown), 1) * rowH - gap);

  function style(i: Item): string {
    return `left:${i.x * colW}px;top:${i.y * rowH}px;width:${i.w * colW - gap}px;height:${i.h * rowH - gap}px`;
  }

  function begin(e: PointerEvent, id: string, mode: 'move' | 'size'): void {
    if (e.button !== 0) return;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    drag = { id, mode, x: e.clientX, y: e.clientY, start: items, live: items };
    e.preventDefault();
  }

  function move(e: PointerEvent): void {
    if (!drag) return;
    const dx = Math.round((e.clientX - drag.x) / colW);
    const dy = Math.round((e.clientY - drag.y) / rowH);
    const from = drag.start.find((i) => i.id === drag!.id)!;
    drag.live =
      drag.mode === 'move'
        ? place(drag.start, drag.id, { x: from.x + dx, y: from.y + dy })
        : place(drag.start, drag.id, { w: Math.max(from.w + dx, 2), h: Math.max(from.h + dy, 1) });
  }

  function end(): void {
    if (!drag) return;
    const result = drag.live;
    drag = null;
    onchange(result);
  }
</script>

<div class="area" class:stacked={narrow} class:edit bind:clientWidth={width} style={narrow ? '' : `height:${height}px`}>
  {#each narrow ? items : shown as item (item.id)}
    <div class="cell" class:dragging={drag?.id === item.id} style={narrow ? '' : style(item)}>
      {@render cell(item.id, narrow)}
      {#if edit && !narrow}
        <div class="cover" onpointerdown={(e) => begin(e, item.id, 'move')} onpointermove={move} onpointerup={end}
          onpointercancel={end} role="presentation"></div>
        {#if onremove}
          <button class="remove" onclick={() => onremove(item.id)} aria-label={removeLabel} title={removeLabel}>×</button>
        {/if}
        <div class="size" onpointerdown={(e) => begin(e, item.id, 'size')} onpointermove={move} onpointerup={end}
          onpointercancel={end} role="presentation"></div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .area { position: relative; }
  .area.stacked { display: grid; gap: 12px; }
  .cell { position: absolute; transition: left 0.12s, top 0.12s, width 0.12s, height 0.12s; }
  .stacked .cell { position: static; }
  .cell.dragging { z-index: 3; transition: none; opacity: 0.92; }
  .cover, .size { position: absolute; touch-action: none; }
  .cover { inset: 0; cursor: move; border-radius: var(--radius); outline: 2px dashed var(--accent); outline-offset: -2px; background: color-mix(in srgb, var(--accent) 6%, transparent); }
  .remove { position: absolute; top: 6px; right: 6px; z-index: 3; width: 28px; height: 28px; padding: 0; line-height: 1; font-size: 1.1rem; border-radius: 50%; }
  .size { right: 0; bottom: 0; width: 30px; height: 30px; cursor: nwse-resize; z-index: 2; }
  .size::after { content: ''; position: absolute; right: 6px; bottom: 6px; width: 12px; height: 12px; border-right: 3px solid var(--accent); border-bottom: 3px solid var(--accent); border-radius: 0 0 4px 0; }
</style>
