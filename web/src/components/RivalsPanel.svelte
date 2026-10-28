<script lang="ts">
  import { fixed, signed } from '../lib/format';
  import { tileInfo } from '../lib/info.svelte';
  import { t } from '../lib/settings.svelte';
  import { isAI, shortName, stopLaps } from '../lib/strategy';
  import { live } from '../lib/store';
  import type { Rival } from '../lib/types';

  const s = $derived(live.state);
  const strategy = $derived(s?.strategy);
  const rejoin = $derived(strategy?.rejoin);
  const me = $derived(s?.car);

  type Row = { me: true; place: number } | ({ me: false } & Rival);
  const rows = $derived.by((): Row[] => {
    if (!strategy || !me) return [];
    const list: Row[] = strategy.rivals.map((r) => ({ me: false as const, ...r }));
    list.push({ me: true, place: me.place });
    return list.sort((a, b) => a.place - b.place);
  });

  tileInfo(() => (strategy ? t('rivals.help') : null));
  tileInfo(() => (rejoin?.incomplete ? t('rivals.incomplete') : null));

  const best = $derived(rejoin?.window.find((w) => w.in_laps === rejoin.best_in));
  const gapText = (g: number | null) => (g == null ? '–' : `${g > 0 ? '▲' : '▼'} ${fixed(Math.abs(g), 1)}`);
  const rateTone = (r: number | null) => (r == null ? '' : r > 0.1 ? 'good' : r < -0.1 ? 'bad' : '');
  const stopText = (r: Rival) => {
    const n = stopLaps(r);
    if (n == null) return '–';
    return `${r.stop_basis === 'fuel' ? '' : '≈ '}${n}`;
  };
</script>

{#if !strategy}
  <p class="muted">{t('rivals.none')}</p>
{:else}
  {#if rejoin}
    <div class="rejoin">
      <div>
        {t('rivals.stopNow', { pos: rejoin.position, lost: rejoin.lost, s: fixed(rejoin.loss, 0) })}
        {#if rejoin.passed_by?.length}<span class="muted small">{t('rivals.passedBy', { names: rejoin.passed_by.join(', ') })}</span>{/if}
      </div>
      {#if rejoin.best_in > 0 && best}
        <div class="best">{t('rivals.bestWindow', { n: rejoin.best_in, lost: best.lost })}</div>
      {/if}
      {#if rejoin.also_stop?.length}<div class="muted small">{t('rivals.alsoStop', { names: rejoin.also_stop.join(', ') })}</div>{/if}
    </div>
  {:else}
    <p class="muted small">{t('rivals.noLoss')}</p>
  {/if}

  <table>
    <thead>
      <tr>
        <th>P</th><th>{t('rivals.driver')}</th><th>{t('rivals.gap')}</th><th title={t('rivals.rateHelp')}>{t('rivals.rate')}</th>
        <th>{t('rivals.stops')}</th><th>{t('rivals.stint')}</th><th>{t('rivals.fuel')}</th><th title={t('rivals.tyresHelp')}>{t('rivals.tyres')}</th><th>{t('rivals.stopIn')}</th>
      </tr>
    </thead>
    <tbody>
      {#each rows as r (r.me ? 'me' : r.car_id + r.vehicle_name)}
        {#if r.me}
          <tr class="me"><td>{r.place}</td><td colspan="8">{t('rivals.you')}</td></tr>
        {:else}
          <tr class:pit={r.in_pits}>
            <td>{r.place}</td>
            <td class="name">{shortName(r.driver) || r.vehicle_name}{#if isAI(r)} <span class="ai">{t('rivals.ai')}</span>{/if}{#if r.in_pits} <span class="muted small">{t('map.pit')}</span>{/if}</td>
            <td class="num">{gapText(r.gap)}</td>
            <td class="num {rateTone(r.rate)}">{r.rate == null ? '–' : signed(r.rate, 2)}</td>
            <td class="num">{r.stops}</td>
            <td class="num">{r.stint_laps}</td>
            <td class="num">{r.fuel == null ? '–' : fixed(r.fuel, 0)}</td>
            <td class="num">{r.wear && r.wear.length ? `${Math.round(Math.min(...r.wear) * 100)} %` : '–'}</td>
            <td class="num">{stopText(r)}</td>
          </tr>
        {/if}
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .rejoin { background: var(--surface-2); border-radius: 10px; padding: 8px 10px; margin-bottom: 8px; display: grid; gap: 2px; }
  .best { color: var(--accent); font-weight: 600; }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: 0.7rem; padding: 2px 6px 2px 0; white-space: nowrap; }
  td { padding: 2px 6px 2px 0; border-top: 1px solid var(--border); white-space: nowrap; }
  .name { max-width: 9rem; overflow: hidden; text-overflow: ellipsis; }
  tr.me td { background: color-mix(in srgb, var(--accent) 18%, transparent); font-weight: 600; }
  tr.pit { opacity: 0.6; }
  .ai { font-size: 0.65rem; font-weight: 700; padding: 0 5px; border-radius: 999px; background: var(--warn, #e5a50a); color: #231600; }
  .good { color: var(--good, #3fb950); }
  .bad { color: var(--bad, #e5534b); }
  .small { font-size: 0.75rem; }
</style>
