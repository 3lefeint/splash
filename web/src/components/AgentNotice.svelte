<script lang="ts">
  import { agents } from '../lib/agent.svelte';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  // The backend has no agent to show this member: none of theirs has connected, or none is tied to their address.
  const none = $derived(live.state?.agent_id === -1 && agents.selected === null);
</script>

{#if none}
  <div class="notice" role="status" aria-live="polite">
    <span>{t('picker.none')}</span>
    <span class="muted">{agents.list.length > 0 ? t('picker.noneChoose') : t('picker.noneAsk')}</span>
  </div>
{/if}

<style>
  .notice { grid-column: 1 / -1; display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; padding: 8px 12px; border-radius: 8px; background: var(--surface-2); border-left: 5px solid var(--warn, #e5a50a); }
</style>
