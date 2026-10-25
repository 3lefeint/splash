<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    title,
    tone = 'default',
    wide = false,
    children,
  }: { title: string; tone?: 'default' | 'good' | 'warn' | 'bad'; wide?: boolean; children: Snippet } = $props();
</script>

<section class="tile {tone}" class:wide>
  <h2>{title}</h2>
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
  }
  .tile.warn { border-color: var(--warn); }
  .tile.bad { border-color: var(--bad); }
  .tile.good { border-color: var(--good); }
  h2 { color: var(--muted); font-size: 0.8rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; }
  @media (min-width: 900px) {
    .wide { grid-column: span 2; }
  }
</style>
