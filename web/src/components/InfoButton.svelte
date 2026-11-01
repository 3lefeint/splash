<script lang="ts">
  import type { Component } from 'svelte';
  import { t } from '../lib/settings.svelte';

  let { texts, figure }: { texts: string[]; figure?: Component | null } = $props();

  let open = $state(false);
  let button = $state<HTMLButtonElement>();
  let popup = $state<HTMLDivElement>();
  let pos = $state({ top: 0, left: 0 });

  function toggle(): void {
    if (!open && button) {
      const r = button.getBoundingClientRect();
      const width = Math.min(figure ? 440 : 320, window.innerWidth - 16);
      pos = { top: r.bottom + 6, left: Math.min(Math.max(r.right - width, 8), window.innerWidth - width - 8) };
    }
    open = !open;
  }

  // A press anywhere else, or Escape, closes the popup.
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

  $effect(() => {
    if (texts.length === 0 && !figure) open = false;
  });
</script>

<button bind:this={button} class="i" class:on={open} onclick={toggle} aria-expanded={open} aria-label={t('info.label')} title={t('info.label')}>i</button>
{#if open}
  <div bind:this={popup} class="pop" class:wide={!!figure} role="dialog" style="top:{pos.top}px; left:{pos.left}px">
    {#if figure}{@const Figure = figure}<Figure />{/if}
    {#each texts as text}<p>{text}</p>{/each}
  </div>
{/if}

<style>
  .i {
    width: 30px;
    height: 30px;
    min-width: 30px;
    min-height: 30px;
    padding: 0;
    border-radius: 50%;
    font-size: 0.8rem;
    font-weight: 700;
    font-style: italic;
    font-family: Georgia, serif;
    line-height: 1;
    color: var(--muted);
    background: transparent;
    border: 1px solid var(--border);
    flex: none;
  }
  .i:hover, .i.on { color: var(--accent); border-color: var(--accent); }
  .pop {
    position: fixed;
    z-index: 50;
    width: min(320px, calc(100vw - 16px));
    background: var(--surface-2);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 10px 12px;
    font-size: 0.85rem;
    line-height: 1.4;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
  }
  .pop.wide { width: min(440px, calc(100vw - 16px)); }
  .pop p { margin: 0 0 8px; }
  .pop p:last-child { margin: 0; }
</style>
