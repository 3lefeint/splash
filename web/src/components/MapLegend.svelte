<script lang="ts">
  import { targetName } from '../lib/overlays';
  import { settings, t } from '../lib/settings.svelte';
  import { view } from '../lib/view.svelte';

  /** One line that says what the colour of the map means now. */
  const waitForBase = $derived(settings.mapOverlay === 'delta' && !view.state?.wear_stats?.base);
  const text = $derived(
    waitForBase
      ? t('wear.deltaWait')
      : settings.mapOverlay === 'time'
        ? t(settings.mapTarget === 'best' ? 'map.legend.timeBest' : 'map.legend.time', { name: targetName(view.state, settings.mapTarget) })
        : settings.mapOverlay === 'fuel' || settings.mapOverlay === 'speed' || settings.mapOverlay === 'brake'
          ? t(`map.legend.${settings.mapOverlay}` as 'map.legend.fuel')
          : '',
  );
</script>

{#if text}<span class="muted legend">{text}</span>{/if}

<style>
  .legend { font-size: 0.85rem; }
</style>
