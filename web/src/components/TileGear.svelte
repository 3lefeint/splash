<script module lang="ts">
  /** Which tile has its settings panel open; one at a time, kept when the gear is swapped for the one of the edit mode. */
  const ui = $state<{ open: string | null }>({ open: null });
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Key } from '../lib/i18n';
  import { dashboardLayout } from '../lib/layout.svelte';
  import { t } from '../lib/settings.svelte';
  import { MODES, TILE_LINES } from '../lib/tiles';

  /** The gear of a dashboard tile: a panel at the edge of the window, away from the tile, so the tile stays in view. */
  let { id, children }: { id: string; children?: Snippet } = $props();

  let button = $state<HTMLButtonElement>();
  let side = $state<'left' | 'right'>('right');

  const open = $derived(ui.open === id);
  const options = $derived(dashboardLayout.optionsOf(id));
  const lines = $derived(TILE_LINES[id] ?? []);

  const toggle = () => (ui.open = open ? null : id);

  // The panel goes to the half of the window the tile is not in; the tile gets a frame while it is open.
  $effect(() => {
    if (!open || !button) return;
    const host = button.closest('.cell, .tile') as HTMLElement | null;
    const tile = (host?.matches('.tile') ? host : host?.querySelector('.tile')) as HTMLElement | null;
    if (host) {
      const r = host.getBoundingClientRect();
      side = r.left + r.width / 2 < window.innerWidth / 2 ? 'right' : 'left';
    }
    tile?.classList.add('gear-open');
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') ui.open = null;
    };
    window.addEventListener('keydown', key);
    return () => {
      tile?.classList.remove('gear-open');
      window.removeEventListener('keydown', key);
    };
  });
</script>

<button bind:this={button} class="gear" class:on={open} onclick={toggle} aria-expanded={open} aria-label={t('tile.gear')} title={t('tile.gear')}>
  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </svg>
</button>
{#if open}
  <div class="pop {side}" role="dialog" aria-label={t('tile.gear')}>
    <div class="head">
      <b>{t('tile.gear')}</b>
      <button class="close" onclick={() => (ui.open = null)} aria-label={t('tile.close')} title={t('tile.close')}>×</button>
    </div>
    {#if lines.length}<div class="group">
      <span class="label">{t('tile.mode')}</span>
      <span class="sizes">
        {#each MODES as mode (mode)}
          <button class:on={(options.mode ?? 'normal') === mode} onclick={() => dashboardLayout.setOption(id, { mode })}>{t(`tile.mode.${mode}` as Key)}</button>
        {/each}
      </span>
    </div>{/if}
    {#if lines.length}
      <div class="group col">
        <span class="label">{t('tile.lines')}</span>
        {#each lines as line (line)}
          <label class="line">
            <input type="checkbox" checked={dashboardLayout.shows(id, line)} onchange={() => dashboardLayout.toggleLine(id, line)} />
            {t(`line.${id}.${line}` as Key)}
          </label>
        {/each}
      </div>
    {/if}
    {#if children}{@render children()}{/if}
  </div>
{/if}

<style>
  .gear {
    width: 30px; height: 30px; min-width: 30px; min-height: 30px; padding: 0; flex: none;
    display: inline-flex; align-items: center; justify-content: center;
    border-radius: 50%; color: var(--muted); background: transparent; border: 1px solid var(--border);
  }
  .gear:hover, .gear.on { color: var(--accent); border-color: var(--accent); }
  .pop {
    position: fixed; z-index: 50; top: 64px; bottom: 12px; width: min(300px, calc(100vw - 24px)); overflow: auto;
    display: flex; flex-direction: column; gap: 12px; align-content: flex-start;
    background: var(--surface-2); color: var(--text); border: 1px solid var(--accent); border-radius: 10px;
    padding: 10px 12px; font-size: 0.85rem; line-height: 1.4; box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
  }
  .pop.left { left: 12px; }
  .pop.right { right: 12px; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .close { width: 28px; height: 28px; min-width: 28px; min-height: 28px; padding: 0; line-height: 1; border-radius: 50%; }
  /* On a narrow window the panel is a sheet at the bottom. */
  @media (max-width: 639px) {
    .pop, .pop.left, .pop.right { top: auto; left: 8px; right: 8px; bottom: 8px; width: auto; max-height: 45vh; }
  }
  :global(.tile.gear-open) { outline: 2px solid var(--accent); outline-offset: -2px; }
  .group { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .group.col { flex-direction: column; align-items: flex-start; gap: 6px; }
  .label { color: var(--muted); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .sizes { display: inline-flex; gap: 4px; }
  .sizes button { padding: 2px 10px; }
  .sizes button.on { background: var(--accent); color: #04121f; border-color: var(--accent); }
  .line { display: flex; align-items: center; gap: 8px; cursor: pointer; }
</style>
