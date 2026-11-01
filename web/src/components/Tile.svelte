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
    position: relative;
  }
  /* The numbers inside follow the width of the tile, not of the window. */
  .body { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 6px; container-type: inline-size; }
  /* One line: small title, the values side by side. */
  .tile.compact { padding: 8px 12px; gap: 2px; overflow: hidden; }
  .tile.compact h2 { font-size: 0.65rem; }
  :where(.tile.compact) .body { flex-direction: row; flex-wrap: wrap; align-items: baseline; column-gap: 12px; row-gap: 0; }
  /* Only the first value; the title is hidden and the tools sit in the corner. */
  .tile.value { padding: 6px 10px; overflow: hidden; }
  .tile.value header { position: absolute; top: 4px; right: 6px; opacity: 0.7; z-index: 1; }
  .tile.value h2 { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .tile.value .body > :global(:not(:first-child)) { display: none; }
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
