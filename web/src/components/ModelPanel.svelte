<script lang="ts">
  import { putParams } from '../lib/api';
  import { fixed } from '../lib/format';
  import { tileInfo } from '../lib/info.svelte';
  import { excludedByReason, fitSettingsFor } from '../lib/fit';
  import { dashboardLayout } from '../lib/layout.svelte';
  import { t } from '../lib/settings.svelte';
  import { applyFit, fitDiffers } from '../lib/strategy';
  import { view } from '../lib/view.svelte';
  import type { Key } from '../lib/i18n';

  const SETTING_LABEL = {
    fit_min_laps: 'set.fitMinLaps',
    fit_slow_percent: 'set.fitSlowPercent',
    fit_skip_fcy: 'set.fitSkipFcy',
    fit_skip_wet: 'set.fitSkipWet',
  } as const;
  /** Below this share of explained variation the fit is shown with a warning. */
  const WEAK = 0.5;
  const fit = $derived(view.state?.fit);
  const params = $derived(view.state?.params);
  const canApply = $derived(!!fit && !fit.reason && !!params && !!params.track && fitDiffers(params, fit));
  let busy = $state(false);
  let error = $state('');

  const settingNames = $derived(fitSettingsFor(fit).map((k) => t(SETTING_LABEL[k])).join(', '));
  const groups = $derived(excludedByReason(fit));

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
  <p class="muted">{t(fit.reason === 'few_laps' ? 'model.fewLaps' : 'model.collinear', { n: fit.laps, min: fit.min_laps ?? 8 })}</p>
  {#if fit.reason === 'few_laps' && groups.length > 0}
    <details class="excluded">
      <summary>{t('model.excluded', { n: fit.excluded?.length ?? 0 })}</summary>
      <ul class="why">
        {#each groups as g (g.reason)}
          <li><b>{t(`model.ex.${g.reason}` as Key)}</b>: {g.laps.join(', ')}</li>
        {/each}
      </ul>
      <p class="muted small">{t('model.changeBy', { names: settingNames })}</p>
    </details>
  {/if}
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
  {#if fit.history_laps && fit.scope}
    <div class="muted small">{t(fit.scope === 'team' ? 'model.history.team' : 'model.history.driver', { n: fit.history_laps })}</div>
  {/if}
  {#if fit.r2 < WEAK}<p class="weak">{t('model.weak')}</p>{/if}
  {#if dashboardLayout.shows('model', 'apply')}
    <button class="primary" disabled={!canApply || busy} onclick={apply}>{t('model.apply')}</button>
    {#if error}<p class="err">{error}</p>{/if}
  {/if}
{/if}

<style>
  ul { list-style: none; margin: 0 0 calc(6 * var(--ru) / 16); padding: 0; display: grid; gap: calc(2 * var(--ru) / 16); }
  li { display: grid; grid-template-columns: 1fr auto auto; gap: calc(8 * var(--ru) / 16); align-items: baseline; }
  .small { font-size: calc(0.8 * var(--ru)); margin-bottom: calc(6 * var(--ru) / 16); }
  .weak { color: var(--warn, #e5a50a); font-size: calc(0.85 * var(--ru)); margin: 0 0 calc(6 * var(--ru) / 16); }
  .excluded { font-size: calc(0.85 * var(--ru)); margin-bottom: calc(6 * var(--ru) / 16); }
  .excluded summary { cursor: pointer; color: var(--muted); }
  .why { display: grid; gap: calc(2 * var(--ru) / 16); margin: calc(6 * var(--ru) / 16) 0; }
  .why li { display: block; }
  .err { color: var(--bad, #e5534b); }
</style>
