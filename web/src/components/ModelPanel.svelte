<script lang="ts">
  import { putParams } from '../lib/api';
  import { fixed } from '../lib/format';
  import { tileInfo } from '../lib/info.svelte';
  import { t } from '../lib/settings.svelte';
  import { applyFit, fitDiffers } from '../lib/strategy';
  import { live } from '../lib/store';

  /** Below this share of explained variation the fit is shown with a warning. */
  const WEAK = 0.5;
  const fit = $derived(live.state?.fit);
  const params = $derived(live.state?.params);
  const canApply = $derived(!!fit && !fit.reason && !!params && !!params.track && fitDiffers(params, fit));
  let busy = $state(false);
  let error = $state('');

  tileInfo(() => (fit ? t('model.help') : null));

  async function apply(): Promise<void> {
    if (!fit || !params) return;
    busy = true;
    error = '';
    try {
      await putParams(applyFit(params, fit));
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    } finally {
      busy = false;
    }
  }
</script>

{#if !fit}
  <p class="muted">{t('model.none')}</p>
{:else if fit.reason}
  <p class="muted">{t(fit.reason === 'few_laps' ? 'model.fewLaps' : 'model.collinear', { n: fit.laps })}</p>
{:else}
  <ul>
    <li>
      <span>{t('model.fuelEffect')}</span>
      <b class="num">{fit.fuel_effect == null ? '–' : `${fixed(fit.fuel_effect, 3)} s/L`}</b>
      {#if params}<span class="muted">({fixed(params.fuel_effect, 3)})</span>{/if}
    </li>
    <li>
      <span>{t('model.deg')}</span>
      <b class="num">{fit.deg_per_lap == null ? '–' : `${fixed(fit.deg_per_lap, 3)} s/${t('model.lap')}`}</b>
      {#if params}<span class="muted">({fixed(params.deg_per_lap, 3)})</span>{/if}
    </li>
  </ul>
  <div class="muted small">{t('model.basis', { n: fit.laps, r2: fixed(fit.r2 * 100, 0) })}</div>
  {#if fit.r2 < WEAK}<p class="weak">{t('model.weak')}</p>{/if}
  <button class="primary" disabled={!canApply || busy} onclick={apply}>{t('model.apply')}</button>
  {#if error}<p class="err">{error}</p>{/if}
{/if}

<style>
  ul { list-style: none; margin: 0 0 6px; padding: 0; display: grid; gap: 2px; }
  li { display: grid; grid-template-columns: 1fr auto auto; gap: 8px; align-items: baseline; }
  .small { font-size: 0.8rem; margin-bottom: 6px; }
  .weak { color: var(--warn, #e5a50a); font-size: 0.85rem; margin: 0 0 6px; }
  .err { color: var(--bad, #e5534b); }
</style>
