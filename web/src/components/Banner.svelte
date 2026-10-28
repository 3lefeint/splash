<script lang="ts">
  import { announcer } from '../lib/announcer.svelte';
  import type { Key } from '../lib/i18n';
  import { t } from '../lib/settings.svelte';
</script>

{#if announcer.active.length > 0}
  <div class="banner" role="status" aria-live="polite">
    {#each announcer.active as a (a.id)}
      <span class="item {a.level}">{t(a.key as Key, a.params)}</span>
    {/each}
  </div>
{/if}

<style>
  .banner { grid-column: 1 / -1; display: flex; flex-wrap: wrap; gap: 8px; padding-top: 4px; }
  .item { padding: 6px 12px; border-radius: 8px; font-weight: 600; background: var(--surface-2); border-left: 5px solid var(--accent); }
  .item.warn { border-left-color: var(--warn, #e5a50a); }
  .item.bad { border-left-color: var(--bad, #e5534b); background: color-mix(in srgb, var(--bad, #e5534b) 22%, var(--surface-2)); }
</style>
