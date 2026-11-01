<script lang="ts">
  import { agents } from '../lib/agent.svelte';
  import { t } from '../lib/settings.svelte';

  const hasMine = $derived(agents.list.some((a) => a.mine));
  // One agent that is the member's own needs no choice; anything else does.
  const show = $derived(agents.list.length > 1 || (agents.list.length === 1 && !hasMine) || agents.selected !== null);

  function change(e: Event): void {
    const v = (e.currentTarget as HTMLSelectElement).value;
    agents.select(v === '' ? null : Number(v));
  }
</script>

{#if show}
  <select class="picker" value={agents.selected === null ? '' : String(agents.selected)} onchange={change} aria-label={t('picker.label')} title={t('picker.label')}>
    <option value="">{hasMine ? t('picker.mine') : t('picker.choose')}</option>
    {#each agents.list as a (a.id)}
      <option value={String(a.id)}>{a.name}{a.mine ? ' ★' : ''}{a.active ? ` · ${t('picker.live')}` : ''}</option>
    {/each}
  </select>
{/if}

<style>
  .picker { min-height: 32px; max-width: 12rem; font-size: 0.8rem; color: var(--muted); }
</style>
