<script lang="ts">
  import { t } from '../lib/settings.svelte';

  let { texts }: { texts: string[] } = $props();

  let open = $state(false);
  let button = $state<HTMLButtonElement>();
  let popup = $state<HTMLDivElement>();
  let pos = $state({ top: 0, right: 0 });

  function toggle(): void {
    if (!open && button) {
      const r = button.getBoundingClientRect();
      pos = { top: r.bottom + 6, right: Math.max(window.innerWidth - r.right, 8) };
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
    if (texts.length === 0) open = false;
  });
</script>

<button bind:this={button} class="i" class:on={open} onclick={toggle} aria-expanded={open} aria-label={t('info.label')} title={t('info.label')}>i</button>
{#if open}
  <div bind:this={popup} class="pop" role="dialog" style="top:{pos.top}px; right:{pos.right}px">
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
  .pop p { margin: 0 0 8px; }
  .pop p:last-child { margin: 0; }
</style>
