<script lang="ts">
  import type { Snippet } from 'svelte';
  import { provideTileInfo } from '../lib/info.svelte';
  import { dashboardLayout } from '../lib/layout.svelte';
  import { SIZE_SCALE } from '../lib/tiles';
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
  const scale = $derived(SIZE_SCALE[(id && dashboardLayout.optionsOf(id).size) || 'm']);
</script>

<section class="tile {tone}" class:wide style="--ts:{scale}">
  <header>
    <h2>{title}</h2>
    <span class="tools">
      {#if info.list.length > 0}<InfoButton texts={info.list} />{/if}
      {#if action}{@render action()}{/if}
      {#if gear && id && !dashboardLayout.edit}{@render gear(id ?? '')}{/if}
    </span>
  </header>
  <div class="body">{@render children()}</div>
</section>

<style>
  .tile {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    height: 100%;
    overflow: auto;
    font-size: calc(1rem * var(--ts, 1));
  }
  /* The numbers inside follow the width of the tile, not of the window. */
  .body { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 6px; container-type: inline-size; }
  .tile.warn { border-color: var(--warn); }
  .tile.bad { border-color: var(--bad); }
  .tile.good { border-color: var(--good); }
  .tools { display: flex; align-items: center; gap: 8px; }
  header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  h2 { color: var(--muted); font-size: 0.8rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; }
  @media (min-width: 900px) {
    .wide { grid-column: span 2; }
  }
</style>
