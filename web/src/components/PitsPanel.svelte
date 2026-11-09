<script lang="ts">
  import { putParams } from '../lib/api';
  import { fixed } from '../lib/format';
  import { applySuggestion, differs } from '../lib/pits';
  import { tileInfo } from '../lib/info.svelte';
  import type { Key } from '../lib/i18n';
  import { dashboardLayout } from '../lib/layout.svelte';
  import { t } from '../lib/settings.svelte';
  import { view } from '../lib/view.svelte';

  const p = $derived(view.state?.pits);
  const params = $derived(view.state?.params);
  const sug = $derived(p?.suggest);
  const canApply = $derived(!!sug && !!params && !!params.track && differs(params, sug));
  tileInfo(() => (p && !sug ? t('pits.waitingStand') : null));
  let busy = $state(false);
  let error = $state('');
  const sec = (v: number | null) => (v == null ? '–' : fixed(v, 1));

  async function apply(): Promise<void> {
    if (!sug || !params) return;
    busy = true;
    error = '';
    try {
      await putParams(applySuggestion(params, sug));
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    } finally {
      busy = false;
    }
  }
</script>

{#if !p}
  <p class="muted">{t('pits.none')}</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>#</th><th>{t('pits.box')}</th><th>{t('pits.work')}</th><th>{t('pits.lane')}</th><th title={t('pits.lossHelp')}>{t('pits.loss')}</th><th>{t('pits.fuel')}</th><th>{t('pits.service')}</th>
      </tr>
    </thead>
    <tbody>
      {#each [...(p.rows ?? [])].reverse() as r (r.no)}
        <tr>
          <td>{r.no}</td>
          <td class="num">{sec(r.stopped)} s</td>
          <td class="num">{sec(r.work)} s</td>
          <td class="num">{sec(r.lane)} s</td>
          <td class="num" title={t('pits.lossHelp')}>
            {#if r.net == null && r.net_missing}
              <span class="muted small">{t(`pits.missing.${r.net_missing}` as Key)}</span>
            {:else}
              {r.net == null ? '–' : `${sec(r.net)} s`} <span class="muted small">({sec(r.through)} − {sec(r.race_part)})</span>
            {/if}
          </td>
          <td class="num">{r.fuel == null ? '–' : fixed(r.fuel, 0)} L</td>
          <td>{[r.tyres ? t('pits.tyres') : '', r.swap ? t('pits.swap') : ''].filter(Boolean).join(' + ') || '–'}</td>
        </tr>
      {/each}
    </tbody>
  </table>

  {#if sug && params && dashboardLayout.shows('pits', 'suggest')}
    <h3>{t('pits.suggest')}</h3>
    <ul class="sug">
      {#if sug.pit_through != null}<li><span>{t('pits.through')}</span><b class="num">{fixed(sug.pit_through, 1)} s</b><span class="muted">({fixed(params.pit_through_green, 1)})</span></li>{/if}
      {#if sug.refuel_rate != null}<li><span>{t('pits.rate')}</span><b class="num">{fixed(sug.refuel_rate, 2)} L/s</b><span class="muted">({fixed(params.refuel_rate, 2)})</span></li>{/if}
      {#if sug.driver_swap != null}<li><span>{t('pits.swapTime')}</span><b class="num">{fixed(sug.driver_swap, 1)} s</b><span class="muted">({fixed(params.driver_swap, 1)})</span></li>{/if}
    </ul>
    <button class="primary" disabled={!canApply || busy} onclick={apply}>{t('pits.apply')}</button>
    {#if error}<p class="error">{error}</p>{/if}
  {/if}
{/if}

<style>
  table { width: 100%; border-collapse: collapse; font-size: calc(0.9 * var(--ru)); }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: calc(0.75 * var(--ru)); padding: calc(2 * var(--ru) / 16) calc(6 * var(--ru) / 16) calc(2 * var(--ru) / 16) 0; }
  .small { font-size: calc(0.75 * var(--ru)); }
  td { padding: calc(3 * var(--ru) / 16) calc(6 * var(--ru) / 16) calc(3 * var(--ru) / 16) 0; border-top: 1px solid var(--border); white-space: nowrap; }
  h3 { font-size: calc(0.8 * var(--ru)); color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; margin-top: calc(8 * var(--ru) / 16); }
  .sug { list-style: none; margin: 0; padding: 0; display: grid; gap: calc(2 * var(--ru) / 16); }
  .sug li { display: grid; grid-template-columns: 1fr auto auto; gap: calc(8 * var(--ru) / 16); align-items: baseline; }
</style>
