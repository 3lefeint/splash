<script lang="ts">
  import { tileInfo } from '../lib/info.svelte';
  import { targetName, type Target } from '../lib/overlays';
  import { settings, t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import { carNumber, classColor, hideKey } from '../lib/trackmap';
  import type { Overlay, Wheel } from '../lib/wear';

  let { full = false }: { full?: boolean } = $props();

  const field = $derived(live.state?.field ?? []);
  const waitForBase = $derived(settings.mapOverlay === 'delta' && !live.state?.wear_stats?.base);
  const hosted = tileInfo(() => (waitForBase ? t('wear.deltaWait') : null));
  const classNames = $derived([...new Set(field.map((c) => c.class))].sort());
  const cars = $derived(field.filter((c) => !c.ours).sort((a, b) => a.place - b.place));

  function setOverlay(v: Overlay): void {
    settings.mapOverlay = v;
    settings.save();
  }

  function setTarget(v: Target): void {
    settings.mapTarget = v;
    settings.save();
  }

  function setFlag(key: 'mapPace' | 'mapPits' | 'mapGhost'): void {
    settings[key] = !settings[key];
    settings.save();
  }

  const wearOverlay = $derived(settings.mapOverlay === 'wear' || settings.mapOverlay === 'delta');
  const legend = $derived(
    settings.mapOverlay === 'time'
      ? t(settings.mapTarget === 'best' ? 'map.legend.timeBest' : 'map.legend.time', { name: targetName(live.state, settings.mapTarget) })
      : settings.mapOverlay === 'fuel' || settings.mapOverlay === 'speed' || settings.mapOverlay === 'brake'
        ? t(`map.legend.${settings.mapOverlay}` as 'map.legend.fuel')
        : '',
  );

  function setWheel(v: Wheel): void {
    settings.mapWheel = v;
    settings.save();
  }

  function toggleOpponents(): void {
    settings.mapOpponents = !settings.mapOpponents;
    settings.save();
  }
</script>

<div class="controls">
  <div class="group" role="group" aria-label={t('map.overlay')}>
    <span class="label">{t('map.overlay')}</span>
    <div class="seg">
      {#each [['off', 'map.overlayOff'], ['wear', 'map.overlayWear'], ['delta', 'map.overlayDelta'], ['time', 'map.overlayTime'], ['fuel', 'map.overlayFuel'], ['speed', 'map.overlaySpeed'], ['brake', 'map.overlayBrake']] as [v, k] (v)}
        <button class="chip" class:active={settings.mapOverlay === v} aria-pressed={settings.mapOverlay === v}
          onclick={() => setOverlay(v as Overlay)}>{t(k as 'map.overlayOff')}</button>
      {/each}
    </div>
    {#if waitForBase && !hosted}
      <span class="muted hint">{t('wear.deltaWait')}</span>
    {/if}
    {#if settings.mapOverlay === 'time'}
      <div class="seg">
        {#each [['ahead', 'map.targetAhead'], ['behind', 'map.targetBehind'], ['best', 'map.targetBest']] as [v, k] (v)}
          <button class="chip" class:active={settings.mapTarget === v} aria-pressed={settings.mapTarget === v}
            onclick={() => setTarget(v as Target)}>{t(k as 'map.targetAhead')}</button>
        {/each}
      </div>
    {/if}
    {#if legend}<span class="muted hint">{legend}</span>{/if}
    {#if full && wearOverlay}
      <div class="seg">
        {#each [['all', 'all'], ['0', 'FL'], ['1', 'FR'], ['2', 'RL'], ['3', 'RR']] as [v, label] (v)}
          <button class="chip" class:active={settings.mapWheel === v} aria-pressed={settings.mapWheel === v}
            onclick={() => setWheel(v as Wheel)}>{v === 'all' ? t('map.wheelAll') : label}</button>
        {/each}
      </div>
    {/if}
  </div>

  <label class="row">
    <input type="checkbox" checked={settings.mapOpponents} onchange={toggleOpponents} />
    {t('map.opponents')}
  </label>

  {#if full}
    <label class="row"><input type="checkbox" checked={settings.mapPace} onchange={() => setFlag('mapPace')} />{t('map.pace')}</label>
    <label class="row"><input type="checkbox" checked={settings.mapPits} onchange={() => setFlag('mapPits')} />{t('map.pits')}</label>
    <label class="row"><input type="checkbox" checked={settings.mapGhost} onchange={() => setFlag('mapGhost')} />{t('map.ghost')}</label>
  {/if}

  {#if full && settings.mapOpponents}
    <h3>{t('map.classes')}</h3>
    <div class="chips">
      {#each classNames as cls (cls)}
        {@const off = settings.mapHiddenClasses.includes(cls)}
        <button class="chip" class:off aria-pressed={!off} onclick={() => settings.toggleHidden('mapHiddenClasses', cls)}>
          <span class="swatch" style="background:{classColor(classNames, cls)}"></span>{cls || '–'}
        </button>
      {/each}
    </div>

    <details>
      <summary>{t('map.cars')} ({cars.length})</summary>
      <ul>
        {#each cars as c (c.car_id + '|' + c.vehicle_name)}
          <li>
            <label class="row">
              <input
                type="checkbox"
                checked={!settings.mapHiddenCars.includes(hideKey(c))}
                onchange={() => settings.toggleHidden('mapHiddenCars', hideKey(c))}
              />
              <span class="swatch" style="background:{classColor(classNames, c.class)}"></span>
              <span class="num muted">{carNumber(c.vehicle_name)}</span>
              <span class="name">{c.driver || c.vehicle_name}</span>
            </label>
          </li>
        {/each}
      </ul>
    </details>
  {/if}
</div>

<style>
  .controls { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
  .group { display: flex; flex-direction: column; gap: 6px; }
  .label { font-size: 0.8rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; }
  .hint { font-size: 0.85rem; }
  .seg { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip.active { background: var(--accent); color: #04121c; border-color: transparent; font-weight: 600; }
  .row { display: flex; align-items: center; gap: 8px; min-height: 36px; }
  h3 { font-size: 0.8rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; margin-top: 4px; }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip { display: inline-flex; align-items: center; gap: 6px; padding: 0.3rem 0.7rem; min-height: 40px; border-radius: 999px; }
  .chip.off { opacity: 0.45; }
  .swatch { width: 10px; height: 10px; border-radius: 50%; flex: none; }
  summary { cursor: pointer; padding: 8px 0; min-height: 40px; }
  ul { list-style: none; margin: 0; padding: 0; max-height: 320px; overflow-y: auto; }
  .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
