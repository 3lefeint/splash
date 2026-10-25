<script lang="ts">
  import { corners } from '../lib/corners.svelte';
  import { locate } from '../lib/corners';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  const s = $derived(live.state);
  const dist = $derived(s?.field?.find((c) => c.ours)?.lap_dist ?? null);
  const where = $derived(dist != null && s?.track_length ? locate(corners.list, dist, s.track_length) : null);
</script>

{#if !where || (!where.current && !where.next)}
  <p class="muted">{t('corner.none')}</p>
{:else if where.current}
  <div class="name">{where.current.name}</div>
{:else}
  <div class="name muted">–</div>
  <div class="muted small">{t('corner.between')}</div>
{/if}

<style>
  p { margin: 0; }
  .small { font-size: 0.8rem; }
  .name { font-size: clamp(1.8rem, 6vw, 3rem); font-weight: 700; line-height: 1.1; overflow-wrap: anywhere; }
</style>
