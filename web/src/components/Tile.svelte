<script lang="ts">
  import type { Snippet } from 'svelte';
  import { provideTileInfo } from '../lib/info.svelte';
  import InfoButton from './InfoButton.svelte';

  let {
    title,
    tone = 'default',
    wide = false,
    action,
    children,
  }: {
    title: string;
    tone?: 'default' | 'good' | 'warn' | 'bad';
    wide?: boolean;
    /** Optional control in the top right corner of the tile. */
    action?: Snippet;
    children: Snippet;
  } = $props();

  const info = provideTileInfo();
</script>

<section class="tile {tone}" class:wide>
  {#if action || info.list.length > 0}
    <header>
      <h2>{title}</h2>
      <span class="tools">
        {#if info.list.length > 0}<InfoButton texts={info.list} />{/if}
        {#if action}{@render action()}{/if}
      </span>
    </header>
  {:else}
    <h2>{title}</h2>
  {/if}
  {@render children()}
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
  }
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
