<script lang="ts">
  import { live } from '../lib/store';
  import { t } from '../lib/settings.svelte';
</script>

{#if live.authProblem}
  <div class="notice" role="alert">
    <span>{t(live.authProblem === 'expired' ? 'conn.expired' : 'conn.denied')}</span>
    {#if live.authProblem === 'expired'}
      <button class="primary" onclick={() => location.reload()}>{t('conn.login')}</button>
    {/if}
  </div>
{:else if live.reconnecting && live.state}
  <div class="notice soft" role="status" aria-live="polite">{t('conn.stale')}</div>
{/if}

<style>
  .notice { grid-column: 1 / -1; display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 8px 12px; border-radius: 8px; font-weight: 600; background: color-mix(in srgb, var(--bad, #e5534b) 22%, var(--surface-2)); border-left: 5px solid var(--bad, #e5534b); }
  .notice.soft { background: var(--surface-2); border-left-color: var(--warn, #e5a50a); font-weight: 400; }
</style>
