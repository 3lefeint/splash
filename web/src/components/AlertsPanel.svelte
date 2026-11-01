<script lang="ts">
  import { corners } from '../lib/corners.svelte';
  import { tileInfo } from '../lib/info.svelte';
  import { settings, t } from '../lib/settings.svelte';
  import { alerts } from '../lib/strategy';
  import { view } from '../lib/view.svelte';
  import type { Key } from '../lib/i18n';

  const list = $derived(alerts(view.state, settings.tyreRanges, settings.tyreFallback, { yellow: settings.yellowAhead, corners: corners.list }));
  tileInfo(() => t('alerts.help'));
</script>

{#if list.length === 0}
  <p class="muted">{t('alerts.none')}</p>
{:else}
  <ul>
    {#each list as a (a.id)}
      <li class={a.level}>{t(a.key as Key, a.params)}{#if a.small} <small class="muted">{t(a.small.key as Key, a.small.params)}</small>{/if}</li>
    {/each}
  </ul>
{/if}

<style>
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  li { padding: 6px 10px; border-radius: 8px; border-left: 4px solid var(--muted); background: var(--surface-2); font-size: 0.9rem; }
  li.warn { border-left-color: var(--warn, #e5a50a); }
  li.bad { border-left-color: var(--bad, #e5534b); }
  li.info { border-left-color: var(--accent); }
</style>
