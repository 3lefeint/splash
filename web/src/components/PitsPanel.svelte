<script lang="ts">
  import { putParams } from '../lib/api';
  import { fixed } from '../lib/format';
  import { applySuggestion, differs } from '../lib/pits';
  import { tileInfo } from '../lib/info.svelte';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  const p = $derived(live.state?.pits);
  const menu = $derived(live.state?.pit_menu);
  const params = $derived(live.state?.params);
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

{#if menu}
  <p class="menu"><span class="muted">{t('pits.menu')}</span> <b>{menu.category}</b> {menu.choice} <span class="muted">({menu.choice_index + 1}/{menu.choices})</span></p>
{/if}

{#if !p}
  <p class="muted">{t('pits.none')}</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>#</th><th>{t('pits.box')}</th><th>{t('pits.work')}</th><th>{t('pits.lane')}</th><th>{t('pits.fuel')}</th><th>{t('pits.service')}</th>
      </tr>
    </thead>
    <tbody>
      {#each p.rows as r (r.no)}
        <tr>
          <td>{r.no}</td>
          <td class="num">{sec(r.stopped)} s</td>
          <td class="num">{sec(r.work)} s</td>
          <td class="num">{sec(r.lane)} s</td>
          <td class="num">{r.fuel == null ? '–' : fixed(r.fuel, 0)} L</td>
          <td>{[r.tyres ? t('pits.tyres') : '', r.swap ? t('pits.swap') : ''].filter(Boolean).join(' + ') || '–'}</td>
        </tr>
      {/each}
    </tbody>
  </table>

  {#if sug && params}
    <h3>{t('pits.suggest')}</h3>
    <ul class="sug">
      {#if sug.pit_through != null}<li><span>{t('pits.through')}</span><b class="num">{fixed(sug.pit_through, 1)} s</b><span class="muted">({fixed(params.pit_through_green, 1)})</span></li>{/if}
      {#if sug.refuel_rate != null}<li><span>{t('pits.rate')}</span><b class="num">{fixed(sug.refuel_rate, 2)} L/s</b><span class="muted">({fixed(params.refuel_rate, 2)})</span></li>{/if}
      {#if sug.tyre_change != null}<li><span>{t('pits.tyreTime')}</span><b class="num">{fixed(sug.tyre_change, 1)} s</b><span class="muted">({fixed(params.tyre_change, 1)})</span></li>{/if}
      {#if sug.driver_swap != null}<li><span>{t('pits.swapTime')}</span><b class="num">{fixed(sug.driver_swap, 1)} s</b><span class="muted">({fixed(params.driver_swap, 1)})</span></li>{/if}
      {#if sug.parallel_service != null}<li><span>{t('pits.parallel')}</span><b>{sug.parallel_service ? t('pits.yes') : t('pits.no')}</b></li>{/if}
    </ul>
    <button class="primary" disabled={!canApply || busy} onclick={apply}>{t('pits.apply')}</button>
    {#if error}<p class="error">{error}</p>{/if}
  {/if}
{/if}

<style>
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: 0.75rem; padding: 2px 6px 2px 0; }
  td { padding: 3px 6px 3px 0; border-top: 1px solid var(--border); white-space: nowrap; }
  .menu { margin: 0 0 8px; font-size: 0.9rem; }
  h3 { font-size: 0.8rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; margin-top: 8px; }
  .sug { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
  .sug li { display: grid; grid-template-columns: 1fr auto auto; gap: 8px; align-items: baseline; }
</style>
