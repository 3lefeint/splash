<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { Key } from '../lib/i18n';
  import { dashboardLayout } from '../lib/layout.svelte';
  import { t } from '../lib/settings.svelte';
  import { SIZES, TILE_LINES } from '../lib/tiles';

  /** The gear of a dashboard tile: text size, the lines to show and whatever the tile adds itself. */
  let { id, children }: { id: string; children?: Snippet } = $props();

  let open = $state(false);
  let button = $state<HTMLButtonElement>();
  let popup = $state<HTMLDivElement>();
  let pos = $state({ top: 0, left: 0 });

  const options = $derived(dashboardLayout.optionsOf(id));
  const lines = $derived(TILE_LINES[id] ?? []);

  function toggle(): void {
    if (!open && button) {
      const r = button.getBoundingClientRect();
      const width = Math.min(340, window.innerWidth - 16);
      pos = { top: r.bottom + 6, left: Math.min(Math.max(r.right - width, 8), window.innerWidth - width - 8) };
    }
    open = !open;
  }

  $effect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => {
      const target = e.target as Node;
      if (!popup?.contains(target) && !button?.contains(target)) open = false;
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') open = false;
    };
    window.addEventListener('pointerdown', away, true);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('pointerdown', away, true);
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
  <div bind:this={popup} class="pop" role="dialog" style="top:{pos.top}px; left:{pos.left}px">
    <div class="group">
      <span class="label">{t('tile.size')}</span>
      <span class="sizes">
        {#each SIZES as size (size)}
          <button class:on={(options.size ?? 'm') === size} onclick={() => dashboardLayout.setOption(id, { size })}>{t(`tile.size.${size}` as Key)}</button>
        {/each}
      </span>
    </div>
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
    position: fixed; z-index: 50; width: min(340px, calc(100vw - 16px)); max-height: calc(100vh - 80px); overflow: auto;
    display: flex; flex-direction: column; gap: 12px;
    background: var(--surface-2); color: var(--text); border: 1px solid var(--border); border-radius: 10px;
    padding: 10px 12px; font-size: 0.85rem; line-height: 1.4; box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
  }
  .group { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .group.col { flex-direction: column; align-items: flex-start; gap: 6px; }
  .label { color: var(--muted); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .sizes { display: inline-flex; gap: 4px; }
  .sizes button { min-width: 36px; padding: 2px 10px; }
  .sizes button.on { background: var(--accent); color: #04121f; border-color: var(--accent); }
  .line { display: flex; align-items: center; gap: 8px; cursor: pointer; }
</style>
