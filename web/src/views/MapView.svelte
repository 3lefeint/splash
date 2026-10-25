<script lang="ts">
  import MapControls from '../components/MapControls.svelte';
  import TrackMap from '../components/TrackMap.svelte';
  import WearPanel from '../components/WearPanel.svelte';
  import { corners } from '../lib/corners.svelte';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  const track = $derived(live.state?.track);

  // the corner names shown on the map belong to the track of the session
  $effect(() => corners.watch(track));
</script>

<div class="head">
  <h1>{t('map.title')}</h1>
  <span class="muted">{track ?? ''}</span>
</div>

<div class="layout">
  <section class="panel"><TrackMap /></section>
  <aside class="panel">
    <MapControls full />
    <WearPanel />
  </aside>
</div>

<style>
  .head { display: flex; align-items: baseline; gap: 12px; margin-bottom: var(--gap); flex-wrap: wrap; }
  .layout { display: grid; gap: var(--gap); grid-template-columns: 1fr; align-items: start; }
  @media (min-width: 900px) { .layout { grid-template-columns: 1fr 300px; } }
  .panel { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 14px 16px; display: flex; flex-direction: column; gap: 12px; min-width: 0; }
</style>
