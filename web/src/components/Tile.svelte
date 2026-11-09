<script lang="ts">
  import type { Snippet } from 'svelte';
  import { provideTileInfo } from '../lib/info.svelte';
  import { dashboardLayout } from '../lib/layout.svelte';
  import InfoButton from './InfoButton.svelte';

  let {
    title,
    id,
    tone = 'default',
    wide = false,
    action,
    gear,
    children,
  }: {
    title: string;
    /** The id on the dashboard: gives the tile its text size from the layout. */
    id?: string;
    tone?: 'default' | 'good' | 'warn' | 'bad';
    wide?: boolean;
    /** Optional control in the top right corner of the tile. */
    action?: Snippet;
    /** The gear of the tile; hidden while the layout is arranged (the grid shows its own then). */
    gear?: Snippet<[string]>;
    children: Snippet;
  } = $props();

  const info = provideTileInfo();
  const mode = $derived((id && dashboardLayout.optionsOf(id).mode) || 'normal');
</script>

<section class="tile {tone}" data-tile={id} class:wide class:compact={mode === 'compact'} class:value={mode === 'value'} title={mode === 'value' ? title : undefined}>
  <header>
    <h2>{title}</h2>
    <span class="tools">
      {#if info.list.length > 0 || info.figure}<InfoButton texts={info.list} figure={info.figure} />{/if}
      {#if action}{@render action()}{/if}
      {#if gear && id && !dashboardLayout.edit}{@render gear(id ?? '')}{/if}
    </span>
  </header>
  <div class="body">{@render children()}</div>
</section>

<style>
  .tile {
    /* The size of 1rem in this tile: 1rem for a tile of 320 x 160 px (the cell sets --cw and --ch), which is about what the content of a tile needs; smaller or larger with the tile. Without a grid cell (stacked on a narrow screen) it stays 1rem. */
    --ru: min(calc(var(--cw, 320px) * 0.05), calc(var(--ch, 160px) * 0.1));
    font-size: var(--ru);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: calc(14 * var(--ru) / 16) calc(16 * var(--ru) / 16);
    display: flex;
    flex-direction: column;
    gap: calc(6 * var(--ru) / 16);
    min-width: 0;
    height: 100%;
    overflow: auto;
    position: relative;
  }
  /* In the grid the content shrinks with the tile instead of scrolling. */
  :global(.area:not(.stacked)) .tile { overflow: hidden; }
  /* The numbers inside follow the width of the tile, not of the window. */
  .body { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: calc(6 * var(--ru) / 16); container-type: inline-size; }
  /* One line: small title, the values side by side. */
  .tile.compact { padding: calc(8 * var(--ru) / 16) calc(12 * var(--ru) / 16); gap: calc(2 * var(--ru) / 16); overflow: hidden; }
  .tile.compact h2 { font-size: calc(0.65 * var(--ru)); }
  :where(.tile.compact) .body { flex-direction: row; flex-wrap: wrap; align-items: baseline; column-gap: calc(12 * var(--ru) / 16); row-gap: 0; }
  /* Only the first value; the title is hidden and the tools sit in the corner. */
  .tile.value { padding: calc(6 * var(--ru) / 16) calc(10 * var(--ru) / 16); overflow: hidden; }
  .tile.value header { position: absolute; top: 4px; right: 6px; opacity: 0.7; z-index: 1; }
  .tile.value h2 { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .tile.value .body > :global(:not(:first-child)) { display: none; }
  .tile.warn { border-color: var(--warn); }
  .tile.bad { border-color: var(--bad); }
  .tile.good { border-color: var(--good); }
  .tools { display: flex; align-items: center; gap: calc(8 * var(--ru) / 16); }
  header { display: flex; align-items: center; justify-content: space-between; gap: calc(8 * var(--ru) / 16); }
  h2 { color: var(--muted); font-size: calc(0.8 * var(--ru)); font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; }
  @media (min-width: 900px) {
    .wide { grid-column: span 2; }
  }
</style>
