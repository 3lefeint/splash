<script lang="ts">
  import Tile from '../components/Tile.svelte';
  import { duration, fixed, signed, lapTime } from '../lib/format';
  import { trendTone } from '../lib/opponents';
  import { currentEntry, nextEntry, until } from '../lib/plan';
  import { settings, t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import type { Opponent } from '../lib/types';

  const s = $derived(live.state);
  const car = $derived(s?.car);
  const race = $derived(s?.race);
  const plan = $derived(s?.stint_plan);

  const toStop = $derived(race ? until(race.stint_end, live.etNow) : null);
  const timeLeft = $derived(s ? Math.max(s.time_left - (live.etNow - s.et), 0) : null);
  const lowFuel = $derived(race != null && race.laps_to_stop <= 2);
  const saveDelta = $derived(
    race?.save_target != null && car?.fuel_per_lap != null ? race.save_target - car.fuel_per_lap : null,
  );
  const current = $derived(s ? currentEntry(s) : undefined);
  const next = $derived(s ? nextEntry(s) : undefined);
  const tyres = $derived(
    (car?.wear ?? []).map((w) => ({ shown: (settings.wearInverted ? 1 - w : w) * 100, raw: w })),
  );
  const tyreNames = ['FL', 'FR', 'RL', 'RR'];

  function arrow(trend: number | null): string {
    return trend == null || Math.abs(trend) < 0.05 ? '→' : trend < 0 ? '▼' : '▲';
  }
</script>

{#if !s?.ready || !car}
  <div class="wait">
    <h1>{t('wait.title')}</h1>
    <p class="muted">{t('wait.text')}</p>
  </div>
{:else}
  <div class="top">
    <div>
      <span class="muted">{s.track}</span>
      <span class="muted">· {car.vehicle_name}</span>
    </div>
    <div class="num">
      <span class="muted">{t('dash.timeLeft')}</span>
      <strong>{duration(timeLeft)}</strong>
    </div>
  </div>

  <div class="grid">
    <Tile title={t('dash.nextStop')} tone={lowFuel ? 'warn' : 'default'} wide>
      {#if race}
        <div class="big num {lowFuel ? 'warn' : ''}">{duration(toStop)}</div>
        <div class="muted">{t('dash.lapsToStop', { n: race.laps_to_stop })}</div>
      {:else}
        <div class="big muted">–</div>
        <div class="muted">{t('wait.title')}</div>
      {/if}
    </Tile>

    <Tile title={t('dash.fuel')}>
      <div class="big num">
        {fixed(car.fuel, 1)}<span class="unit">L</span>
        {#if car.fuel_overridden}<span class="badge">{t('dash.override')}</span>{/if}
      </div>
      <div class="muted">{t('dash.capacity', { n: fixed(car.fuel_capacity, 0) })}</div>
      <div class="num">
        {t('dash.fuelPerLap')}: <strong>{fixed(car.fuel_per_lap, 2)}</strong> {t('dash.perLap')}
        {#if car.fuel_per_lap_overridden}<span class="badge">{t('dash.override')}</span>{/if}
      </div>
    </Tile>

    <Tile title={t('dash.stops')}>
      {#if race}
        <div class="big num">{race.stops_remaining}</div>
        <div class="muted">{t('dash.lapsLeft', { n: race.laps_left })}</div>
        <div class="num">
          {t('dash.saveTarget')}:
          {#if race.save_target != null}
            <strong>{fixed(race.save_target, 2)}</strong> {t('dash.perLap')}
            {#if saveDelta != null}
              <div class="muted">{t('dash.saveDelta', { n: signed(saveDelta, 2) })}</div>
            {/if}
          {:else}
            <span class="muted">{t('dash.saveNone')}</span>
          {/if}
        </div>
      {:else}
        <div class="big muted">–</div>
      {/if}
    </Tile>

    <Tile title={t('dash.driver')}>
      <div class="name">{car.driver || '–'}</div>
      <div class="num muted">{t('dash.lap', { n: car.lap })} · {t('dash.place', { n: car.place })} · {lapTime(car.lap_time)}</div>
      <div class="tyres" title={t('dash.tyreRaw')}>
        {#each tyres as w, i}
          <div class="tyre" title="{tyreNames[i]}: {fixed(w.raw, 3)} ({t('dash.tyreRaw')})">
            <span class="muted">{tyreNames[i]}</span>
            <span class="num">{fixed(w.shown, 0)}%</span>
          </div>
        {/each}
      </div>
      <div class="muted small">{t('dash.tyres')}</div>
    </Tile>

    <Tile title={t('dash.opponents')}>
      {#each [['ahead', s.opponents.ahead], ['behind', s.opponents.behind]] as [side, o] (side)}
        {@const opp = o as Opponent | null}
        {@const sd = side as 'ahead' | 'behind'}
        <div class="opp">
          <span class="muted">{t(`dash.${sd}` as 'dash.ahead')}</span>
          {#if opp}
            <span class="oppname">{opp.driver || opp.vehicle_name}</span>
            <span class="num">{opp.gap == null ? '–' : `${fixed(opp.gap, 1)} s`}</span>
            <span class="num {trendTone(sd, opp.trend)}">
              {arrow(opp.trend)} {opp.trend == null ? '' : `${signed(opp.trend, 1)} s/${t('dash.lapUnit')}`}
            </span>
          {:else}
            <span class="muted">{t('dash.noOpponent')}</span>
          {/if}
        </div>
      {/each}
    </Tile>

    <Tile title={t('nav.plan')} tone={plan && plan.shortfall > 0 ? 'bad' : 'default'}>
      {#if plan && current}
        <div class="name">{t('dash.stint', { n: current.no })} · {current.driver}</div>
        {#if next}
          <div class="num">{t('dash.next')}: <strong>{next.driver}</strong></div>
        {/if}
        <div class="num {plan.current_deviation > 60 ? 'warn' : plan.current_deviation < -60 ? 'good' : 'muted'}">
          {signed(plan.current_deviation / 60, 0)} min {t('dash.vsPlan')}
        </div>
        {#if plan.shortfall > 0}
          <div class="bad">{t('dash.shortfall', { t: duration(plan.shortfall) })}</div>
        {/if}
      {:else}
        <div class="muted">{t('dash.noPlan')}</div>
      {/if}
    </Tile>
  </div>
{/if}

<style>
  .wait { padding: 48px 8px; text-align: center; }
  .top { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; margin-bottom: var(--gap); flex-wrap: wrap; }
  .top strong { font-size: 1.4rem; margin-left: 6px; }
  .grid { display: grid; gap: var(--gap); grid-template-columns: 1fr; }
  @media (min-width: 640px) { .grid { grid-template-columns: repeat(2, 1fr); } }
  @media (min-width: 900px) { .grid { grid-template-columns: repeat(3, 1fr); } }
  .big { font-size: clamp(2.6rem, 9vw, 4.6rem); font-weight: 700; line-height: 1.05; }
  .unit { font-size: 0.4em; margin-left: 4px; color: var(--muted); font-weight: 500; }
  .name { font-size: 1.3rem; font-weight: 600; }
  .small { font-size: 0.8rem; }
  .badge {
    font-size: 0.7rem; font-weight: 600; padding: 2px 8px; border-radius: 999px; margin-left: 8px;
    background: var(--warn); color: #231600; vertical-align: middle; letter-spacing: 0.03em;
  }
  .tyres { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; margin-top: 4px; }
  .tyre { display: flex; flex-direction: column; align-items: center; background: var(--surface-2); border-radius: 10px; padding: 6px 2px; font-size: 1.1rem; }
  .tyre .muted { font-size: 0.7rem; }
  .opp { display: grid; grid-template-columns: 5.5rem 1fr auto; gap: 2px 10px; align-items: baseline; padding: 6px 0; border-top: 1px solid var(--border); }
  .opp:first-of-type { border-top: 0; }
  .oppname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .opp .num:last-child { grid-column: 2 / 4; text-align: right; }
  .neutral { color: var(--muted); }
</style>
