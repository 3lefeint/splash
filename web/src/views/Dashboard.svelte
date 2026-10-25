<script lang="ts">
  import CornerTile from '../components/CornerTile.svelte';
  import GridLayout from '../components/GridLayout.svelte';
  import MapControls from '../components/MapControls.svelte';
  import AgentsPanel from '../components/AgentsPanel.svelte';
  import AlertsPanel from '../components/AlertsPanel.svelte';
  import ModelPanel from '../components/ModelPanel.svelte';
  import RivalsPanel from '../components/RivalsPanel.svelte';
  import PitSetups from '../components/PitSetups.svelte';
  import PitsPanel from '../components/PitsPanel.svelte';
  import Tile from '../components/Tile.svelte';
  import TrackMap from '../components/TrackMap.svelte';
  import TyreTemps from '../components/TyreTemps.svelte';
  import WeatherPanel from '../components/WeatherPanel.svelte';
  import { corners } from '../lib/corners.svelte';
  import { duration, fixed, signed, lapTime } from '../lib/format';
  import { trendTone } from '../lib/opponents';
  import { setupsUi } from '../lib/setups.svelte';
  import { currentEntry, nextEntry, until } from '../lib/plan';
  import { dashboardLayout } from '../lib/layout.svelte';
  import type { Key } from '../lib/i18n';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import type { Opponent } from '../lib/types';

  const s = $derived(live.state);
  $effect(() => corners.watch(s?.track));
  const car = $derived(s?.car);
  const race = $derived(s?.race);
  const plan = $derived(s?.stint_plan);

  const toStop = $derived(race ? until(race.stint_end, live.etNow) : null);
  const lowFuel = $derived(race != null && race.laps_to_stop <= 2);
  const saveDelta = $derived(
    race?.save_target != null && car?.fuel_per_lap != null ? race.save_target - car.fuel_per_lap : null,
  );
  const current = $derived(s ? currentEntry(s) : undefined);
  const next = $derived(s ? nextEntry(s) : undefined);
  const titles: Record<string, Key> = {
    next: 'dash.nextStop',
    fuel: 'dash.fuel',
    stops: 'dash.stops',
    driver: 'dash.driver',
    opponents: 'dash.opponents',
    plan: 'nav.plan',
    map: 'map.title',
    tyres: 'tyres.title',
    weather: 'weather.title',
    pits: 'pits.title',
    setups: 'setups.title',
    rivals: 'rivals.title',
    alerts: 'alerts.title',
    model: 'model.title',
    agents: 'agents.title',
    corner: 'corner.title',
  };

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
  {#if dashboardLayout.edit}
    <div class="layoutbar no-print">
      <span class="muted hint">{t('layout.hint')}</span>
      <button onclick={() => dashboardLayout.reset()}>{t('layout.reset')}</button>
      <button class="primary" onclick={() => (dashboardLayout.edit = false)}>{t('layout.done')}</button>
    </div>
  {/if}

  {#snippet newSetup()}
    <button class="plus" onclick={() => setupsUi.create()} aria-label={t('setups.new')} title={t('setups.new')}>+</button>
  {/snippet}

  <GridLayout items={dashboardLayout.visible} edit={dashboardLayout.edit} onchange={(i) => dashboardLayout.set(i)} onremove={(id) => dashboardLayout.hide(id)} removeLabel={t('layout.hide')}>
{#snippet cell(id, narrow)}
      {#if id === 'next'}
        <Tile title={t('dash.nextStop')} tone={lowFuel ? 'warn' : 'default'}>
          {#if race}
            <div class="big num {lowFuel ? 'warn' : ''}">{duration(toStop)}</div>
            <div class="muted">{t('dash.lapsToStop', { n: race.laps_to_stop })}</div>
          {:else}
            <div class="big muted">–</div>
            <div class="muted">{t('wait.title')}</div>
          {/if}
        </Tile>
      {/if}
      {#if id === 'fuel'}
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
      {/if}
      {#if id === 'stops'}
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
      {/if}
      {#if id === 'driver'}
        <Tile title={t('dash.driver')}>
          <div class="name">{car.driver || '–'}{#if car.control === 1}<span class="ai">{t('dash.ai')}</span>{/if}</div>
          <div class="num muted">{t('dash.lap', { n: car.lap })} · {t('dash.place', { n: car.place })} · {lapTime(car.lap_time)}</div>
        </Tile>
      {/if}
      {#if id === 'opponents'}
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
      {/if}
      {#if id === 'map'}
        <Tile title={t('map.title')}>
          <div class="mapbox"><TrackMap compact /></div>
          <div class="maprow">
            <MapControls />
            <a href="#/map">{t('map.open')} →</a>
          </div>
        </Tile>
      {/if}
      {#if id === 'tyres'}
        <Tile title={t('tyres.title')}>
          <TyreTemps />
        </Tile>
      {/if}
      {#if id === 'pits'}
        <Tile title={t('pits.title')}>
          <PitsPanel />
        </Tile>
      {/if}
      {#if id === 'setups'}
        <Tile title={t('setups.title')} action={newSetup}>
          <PitSetups />
        </Tile>
      {/if}
      {#if id === 'rivals'}
        <Tile title={t('rivals.title')}>
          <RivalsPanel />
        </Tile>
      {/if}
      {#if id === 'alerts'}
        <Tile title={t('alerts.title')}>
          <AlertsPanel />
        </Tile>
      {/if}
      {#if id === 'agents'}
        <Tile title={t('agents.title')}>
          <AgentsPanel />
        </Tile>
      {/if}
      {#if id === 'model'}
        <Tile title={t('model.title')}>
          <ModelPanel />
        </Tile>
      {/if}
      {#if id === 'corner'}
        <Tile title={t('corner.title')}>
          <CornerTile />
        </Tile>
      {/if}
      {#if id === 'weather'}
        <Tile title={t('weather.title')}>
          <WeatherPanel />
        </Tile>
      {/if}
      {#if id === 'plan'}
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
      {/if}
    {/snippet}
  </GridLayout>
  {#if dashboardLayout.edit && dashboardLayout.hidden.length}
    <div class="hidden">
      <span class="muted">{t('layout.hidden')}</span>
      {#each dashboardLayout.hidden as id (id)}
        <button onclick={() => dashboardLayout.show(id)} title={t('layout.show')}>+ {t(titles[id])}</button>
      {/each}
    </div>
  {/if}
{/if}

<style>
  .wait { padding: 48px 8px; text-align: center; }
  .layoutbar { display: flex; align-items: center; justify-content: flex-end; gap: 10px; margin-bottom: 4px; flex-wrap: wrap; }
  .hidden { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 16px; }
  .layoutbar .hint { margin-right: auto; }
  @media (max-width: 639px) { .layoutbar { display: none; } }
  .mapbox { flex: 1; min-height: 160px; min-width: 0; display: flex; }
  .big { font-size: clamp(2.6rem, 9vw, 4.6rem); font-weight: 700; line-height: 1.05; }
  .unit { font-size: 0.4em; margin-left: 4px; color: var(--muted); font-weight: 500; }
  .name { font-size: 1.3rem; font-weight: 600; }
  .small { font-size: 0.8rem; }
  .badge {
    font-size: 0.7rem; font-weight: 600; padding: 2px 8px; border-radius: 999px; margin-left: 8px;
    background: var(--warn); color: #231600; vertical-align: middle; letter-spacing: 0.03em;
  }
  .ai { margin-left: 8px; font-size: 0.7rem; font-weight: 700; padding: 2px 8px; border-radius: 999px; background: var(--warn, #e5a50a); color: #231600; vertical-align: middle; }
  .plus { padding: 0 10px; font-size: 1.2rem; line-height: 1.6; }
  .opp { display: grid; grid-template-columns: 5.5rem 1fr auto; gap: 2px 10px; align-items: baseline; padding: 6px 0; border-top: 1px solid var(--border); }
  .opp:first-of-type { border-top: 0; }
  .oppname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .opp .num:last-child { grid-column: 2 / 4; text-align: right; }
  .neutral { color: var(--muted); }
  .maprow { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
  .maprow a { color: var(--accent); white-space: nowrap; }
</style>
