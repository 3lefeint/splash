<script lang="ts">
  import CornerTile from '../components/CornerTile.svelte';
  import GridLayout from '../components/GridLayout.svelte';
  import MapControls from '../components/MapControls.svelte';
  import MapLegend from '../components/MapLegend.svelte';
  import AgentsPanel from '../components/AgentsPanel.svelte';
  import AlertsPanel from '../components/AlertsPanel.svelte';
  import ModelPanel from '../components/ModelPanel.svelte';
  import RivalsPanel from '../components/RivalsPanel.svelte';
  import PitSetups from '../components/PitSetups.svelte';
  import PitsPanel from '../components/PitsPanel.svelte';
  import Tile from '../components/Tile.svelte';
  import TileGear from '../components/TileGear.svelte';
  import TrackMap from '../components/TrackMap.svelte';
  import TyreTemps from '../components/TyreTemps.svelte';
  import WeatherPanel from '../components/WeatherPanel.svelte';
  import { corners } from '../lib/corners.svelte';
  import { duration, fixed, signed, lapTime } from '../lib/format';
  import { trendTone } from '../lib/opponents';
  import { setupsUi } from '../lib/setups.svelte';
  import { currentEntry, nextEntry, until } from '../lib/plan';
  import { dashboardLayout } from '../lib/layout.svelte';
  import { identity } from '../lib/me.svelte';
  import { teamDefault } from '../lib/teamdefault.svelte';
  import { hasGear } from '../lib/tiles';
  import type { Key } from '../lib/i18n';
  import { t } from '../lib/settings.svelte';
  import { view } from '../lib/view.svelte';
  import type { Opponent } from '../lib/types';

  const s = $derived(view.state);
  $effect(() => corners.watch(s?.track));
  const car = $derived(s?.car);
  const race = $derived(s?.race);
  const plan = $derived(s?.stint_plan);

  const toStop = $derived(race ? until(race.stint_end, view.etNow) : null);
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
      <label class="demo"><input type="checkbox" checked={view.demo} disabled={!view.hasData} onchange={() => (dashboardLayout.demo = !dashboardLayout.demo)} />{t('layout.demo')}</label>
      <button onclick={() => dashboardLayout.reset()}>{t('layout.reset')}</button>
      {#if identity.isAdmin}
        <button onclick={() => { teamDefault.status = ''; void teamDefault.save(); }}>{t('layout.saveTeam')}</button>
        {#if teamDefault.status}<span class="muted hint" role="status">{t(teamDefault.status === 'done' ? 'layout.saveTeamDone' : 'layout.saveTeamFailed')}</span>{/if}
      {/if}
      <button class="primary" onclick={() => (dashboardLayout.edit = false)}>{t('layout.done')}</button>
    </div>
  {/if}

  {#snippet newSetup()}
    <button class="plus" onclick={() => setupsUi.create()} aria-label={t('setups.new')} title={t('setups.new')}>+</button>
  {/snippet}

  {#snippet gear(id: string)}
    {#if hasGear(id)}<TileGear {id}>
      {#if id === 'map'}<MapControls />{/if}
      {#if id === 'setups'}
        <div class="gearrow">
          <label for="setup-cols">{t('setups.columns')}</label>
          <select id="setup-cols" value={dashboardLayout.optionsOf('setups').columns ?? 2} onchange={(e) => dashboardLayout.setOption('setups', { columns: Number(e.currentTarget.value) })}>
            {#each [1, 2, 3, 4] as n (n)}<option value={n}>{n}</option>{/each}
          </select>
        </div>
        <label class="gearrow"><input type="checkbox" checked={setupsUi.arrange} onchange={() => (setupsUi.arrange = !setupsUi.arrange)} />{t('setups.arrange')}</label>
        <p class="muted small">{t('setups.arrangeHint')}</p>
      {/if}
    </TileGear>{/if}
  {/snippet}

  <GridLayout tools={gear} items={dashboardLayout.visible} edit={dashboardLayout.edit} onchange={(i) => dashboardLayout.set(i)} onremove={(id) => dashboardLayout.hide(id)} removeLabel={t('layout.hide')}>
{#snippet cell(id, narrow)}
      {#if id === 'next'}
        <Tile id="next" {gear} title={t('dash.nextStop')} tone={lowFuel ? 'warn' : 'default'}>
          {#if race}
            <div class="big num {lowFuel ? 'warn' : ''}">{duration(toStop)}</div>
            {#if dashboardLayout.shows('next', 'lapsToStop')}<div class="muted">{t('dash.lapsToStop', { n: race.laps_to_stop })}</div>{/if}
          {:else}
            <div class="big muted">–</div>
            <div class="muted">{t('wait.title')}</div>
          {/if}
        </Tile>
      {/if}
      {#if id === 'fuel'}
        <Tile id="fuel" {gear} title={t('dash.fuel')}>
          <div class="big num">
            {fixed(car.fuel, 1)}<span class="unit">L</span>
            {#if car.fuel_overridden}<span class="badge">{t('dash.override')}</span>{/if}
          </div>
          {#if dashboardLayout.shows('fuel', 'capacity')}<div class="muted">{t('dash.capacity', { n: fixed(car.fuel_capacity, 0) })}</div>{/if}
          {#if dashboardLayout.shows('fuel', 'perLap')}
            <div class="num">
              {t('dash.fuelPerLap')}: <strong>{fixed(car.fuel_per_lap, 2)}</strong> {t('dash.perLap')}
              {#if car.fuel_per_lap_overridden}<span class="badge">{t('dash.override')}</span>{/if}
            </div>
          {/if}
        </Tile>
      {/if}
      {#if id === 'stops'}
        <Tile id="stops" {gear} title={t('dash.stops')}>
          {#if race}
            <div class="big num">{race.stops_remaining}</div>
            {#if dashboardLayout.shows('stops', 'lapsLeft')}<div class="muted s-laps">{t('dash.lapsLeft', { n: race.laps_left })}</div>{/if}
            {#if dashboardLayout.shows('stops', 'saveTarget')}
              <div class="num s-target">
                {t('dash.saveTarget')}:
                {#if race.save_target != null}
                  <strong>{fixed(race.save_target, 2)}</strong> {t('dash.perLap')}
                {:else}
                  <span class="muted">{t('dash.saveNone')}</span>
                {/if}
              </div>
            {/if}
            {#if race.save_target != null && saveDelta != null && dashboardLayout.shows('stops', 'saveDelta')}
              <div class="muted s-delta">{t('dash.saveDelta', { n: signed(saveDelta, 2) })}</div>
            {/if}
          {:else}
            <div class="big muted">–</div>
          {/if}
        </Tile>
      {/if}
      {#if id === 'driver'}
        <Tile id="driver" {gear} title={t('dash.driver')}>
          <div class="name">{car.driver || '–'}{#if car.control === 1}<span class="ai">{t('dash.ai')}</span>{/if}</div>
          {#if dashboardLayout.shows('driver', 'laps')}<div class="num muted">{t('dash.lap', { n: car.lap })} · {t('dash.place', { n: car.place })} · {lapTime(car.lap_time)}</div>{/if}
        </Tile>
      {/if}
      {#if id === 'opponents'}
        <Tile id="opponents" {gear} title={t('dash.opponents')}>
          {#each [['ahead', s.opponents?.ahead], ['behind', s.opponents?.behind]] as [side, o] (side)}
            {@const opp = o as Opponent | null}
            {@const sd = side as 'ahead' | 'behind'}
            <div class="opp">
              <span class="muted">{t(`dash.${sd}` as 'dash.ahead')}</span>
              {#if opp}
                <span class="oppname">{opp.driver || opp.vehicle_name}</span>
                <span class="num">{opp.gap == null ? '–' : `${fixed(opp.gap, 1)} s`}</span>
                {#if dashboardLayout.shows('opponents', 'trend')}
                  <span class="num {trendTone(sd, opp.trend)}">
                    {arrow(opp.trend)} {opp.trend == null ? '' : `${signed(opp.trend, 1)} s/${t('dash.lapUnit')}`}
                  </span>
                {/if}
              {:else}
                <span class="muted">{t('dash.noOpponent')}</span>
              {/if}
            </div>
          {/each}
        </Tile>
      {/if}
      {#if id === 'map'}
        <Tile id="map" {gear} title={t('map.title')}>
          <div class="mapbox"><TrackMap compact /></div>
          <div class="maprow">
            <MapLegend />
            <a href="#/map">{t('map.open')} →</a>
          </div>
        </Tile>
      {/if}
      {#if id === 'tyres'}
        <Tile id="tyres" {gear} title={t('tyres.title')}>
          <TyreTemps />
        </Tile>
      {/if}
      {#if id === 'pits'}
        <Tile id="pits" {gear} title={t('pits.title')}>
          <PitsPanel />
        </Tile>
      {/if}
      {#if id === 'setups'}
        <Tile id="setups" {gear} title={t('setups.title')} action={newSetup}>
          <PitSetups />
        </Tile>
      {/if}
      {#if id === 'rivals'}
        <Tile id="rivals" {gear} title={t('rivals.title')}>
          <RivalsPanel />
        </Tile>
      {/if}
      {#if id === 'alerts'}
        <Tile id="alerts" {gear} title={t('alerts.title')}>
          <AlertsPanel />
        </Tile>
      {/if}
      {#if id === 'agents'}
        <Tile id="agents" {gear} title={t('agents.title')}>
          <AgentsPanel />
        </Tile>
      {/if}
      {#if id === 'model'}
        <Tile id="model" {gear} title={t('model.title')}>
          <ModelPanel />
        </Tile>
      {/if}
      {#if id === 'corner'}
        <Tile id="corner" {gear} title={t('corner.title')}>
          <CornerTile />
        </Tile>
      {/if}
      {#if id === 'weather'}
        <Tile id="weather" {gear} title={t('weather.title')}>
          <WeatherPanel />
        </Tile>
      {/if}
      {#if id === 'plan'}
        <Tile id="plan" {gear} title={t('nav.plan')} tone={plan && plan.shortfall > 0 ? 'bad' : 'default'}>
          {#if plan && current}
            <div class="name">{t('dash.stint', { n: current.no })} · {current.driver}</div>
            {#if next && dashboardLayout.shows('plan', 'next')}
              <div class="num">{t('dash.next')}: <strong>{next.driver}</strong></div>
            {/if}
            {#if dashboardLayout.shows('plan', 'deviation')}
              <div class="num {plan.current_deviation > 60 ? 'warn' : plan.current_deviation < -60 ? 'good' : 'muted'}">
                {signed(plan.current_deviation / 60, 0)} min {t('dash.vsPlan')}
              </div>
            {/if}
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
  .demo { display: flex; align-items: center; gap: 6px; }
  .layoutbar .hint { margin-right: auto; }
  @media (max-width: 639px) { .layoutbar { display: none; } }
  .mapbox { flex: 1; min-height: 160px; min-width: 0; display: flex; }
  :global(.tile.compact) .big { font-size: clamp(calc(1.4 * var(--ru)), 9cqw, calc(2.4 * var(--ru))); }
  :global(.tile.value) .big { font-size: clamp(calc(1.6 * var(--ru)), 24cqw, calc(5 * var(--ru))); }
/* Stops tile, compact: the number on the left spans two lines, laps and target on the first, the difference on the second. */
  :global(.tile.compact[data-tile='stops'] .body) { display: grid; grid-template-columns: auto auto 1fr; grid-template-rows: auto auto; align-items: center; column-gap: calc(14 * var(--ru) / 16); row-gap: 0; }
  :global(.tile.compact[data-tile='stops']) .big { grid-column: 1; grid-row: 1 / 3; font-size: clamp(calc(2 * var(--ru)), 8cqw, calc(3 * var(--ru))); }
  :global(.tile.compact[data-tile='stops']) .s-laps { grid-column: 2; grid-row: 1; }
  :global(.tile.compact[data-tile='stops']) .s-target { grid-column: 3; grid-row: 1; }
  :global(.tile.compact[data-tile='stops']) .s-delta { grid-column: 2 / 4; grid-row: 2; }
  :global(.tile.compact) .name { font-size: 1.05em; }
  .big { font-size: clamp(calc(2 * var(--ru)), 16cqw, calc(4.6 * var(--ru))); font-weight: 700; line-height: 1.05; }
  /* In the grid the numbers follow the size of the tile, width and height; stacked on a narrow screen the rules above stay. */
  :global(.area:not(.stacked) .tile) .big { font-size: calc(3.6 * var(--ru)); }
  :global(.area:not(.stacked) .tile.compact) .big { font-size: calc(2.2 * var(--ru)); }
  :global(.area:not(.stacked) .tile.compact[data-tile='stops']) .big { font-size: calc(2.6 * var(--ru)); }
  :global(.area:not(.stacked) .tile.value) .big { font-size: min(calc(var(--cw, 320px) * 0.2), calc(var(--ch, 300px) * 0.5)); }
  .unit { font-size: 0.4em; margin-left: calc(4 * var(--ru) / 16); color: var(--muted); font-weight: 500; }
  .name { font-size: 1.3em; font-weight: 600; }
  .small { font-size: 0.8em; }
  .badge {
    font-size: calc(0.7 * var(--ru)); font-weight: 600; padding: calc(2 * var(--ru) / 16) calc(8 * var(--ru) / 16); border-radius: 999px; margin-left: calc(8 * var(--ru) / 16);
    background: var(--warn); color: #231600; vertical-align: middle; letter-spacing: 0.03em;
  }
  .ai { margin-left: calc(8 * var(--ru) / 16); font-size: calc(0.7 * var(--ru)); font-weight: 700; padding: calc(2 * var(--ru) / 16) calc(8 * var(--ru) / 16); border-radius: 999px; background: var(--warn, #e5a50a); color: #231600; vertical-align: middle; }
  .gearrow { display: flex; align-items: center; gap: calc(8 * var(--ru) / 16); }
  .plus { padding: 0 calc(10 * var(--ru) / 16); font-size: calc(1.2 * var(--ru)); line-height: 1.6; }
  .opp { display: grid; grid-template-columns: calc(5.5 * var(--ru)) 1fr auto; gap: calc(2 * var(--ru) / 16) calc(10 * var(--ru) / 16); align-items: baseline; padding: calc(6 * var(--ru) / 16) 0; border-top: 1px solid var(--border); }
  .opp:first-of-type { border-top: 0; }
  .oppname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .opp .num:last-child { grid-column: 2 / 4; text-align: right; }
  .neutral { color: var(--muted); }
  .maprow { display: flex; justify-content: space-between; flex-wrap: wrap; align-items: center; gap: calc(12 * var(--ru) / 16); }
  .maprow a { color: var(--accent); white-space: nowrap; }
</style>
